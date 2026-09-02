import base64
import io
import random
import os

import gradio as gr
try:
    import spaces
    HAVE_SPACES = True
except ImportError:
    HAVE_SPACES = False

LOCAL_WORKER = os.environ.get("LOCAL_WORKER", "1") == "1"

import torch
from diffusers import (
    ControlNetModel,
    StableDiffusionControlNetImg2ImgPipeline,
    StableDiffusionControlNetPipeline,
    UniPCMultistepScheduler,
)
from PIL import Image, ImageDraw, ImageFilter, ImageOps


BASE_MODEL = "stable-diffusion-v1-5/stable-diffusion-v1-5"
CONTROLNET_MODEL = "monster-labs/control_v1p_sd15_qrcode_monster"

controlnet = ControlNetModel.from_pretrained(
    CONTROLNET_MODEL,
    subfolder="v2",
    torch_dtype=torch.float16,
    use_safetensors=True,
)
pipe = StableDiffusionControlNetPipeline.from_pretrained(
    BASE_MODEL,
    controlnet=controlnet,
    torch_dtype=torch.float16,
    use_safetensors=True,
    safety_checker=None,
    requires_safety_checker=False,
)
pipe.scheduler = UniPCMultistepScheduler.from_config(pipe.scheduler.config)

# Shared memory img2img pipeline for patch diffusion & harmonization
img2img_pipe = StableDiffusionControlNetImg2ImgPipeline(
    vae=pipe.vae,
    text_encoder=pipe.text_encoder,
    tokenizer=pipe.tokenizer,
    unet=pipe.unet,
    controlnet=pipe.controlnet,
    scheduler=pipe.scheduler,
    safety_checker=None,
    feature_extractor=None,
    requires_safety_checker=False,
)

if torch.cuda.is_available():
    pipe.to("cuda")
    img2img_pipe.to("cuda")
    torch.backends.cuda.matmul.allow_tf32 = True
    torch.backends.cudnn.allow_tf32 = True
    torch.backends.cudnn.benchmark = True
    pipe.enable_vae_slicing()
    img2img_pipe.enable_vae_slicing()
else:
    pipe.enable_model_cpu_offload()
    pipe.enable_attention_slicing()
    img2img_pipe.enable_model_cpu_offload()
    img2img_pipe.enable_attention_slicing()


def gpu_worker(fn):
    return spaces.GPU(duration=120)(fn) if HAVE_SPACES else fn


def decode_base64_image(data: str) -> Image.Image | None:
    if not data or len(data) < 20:
        return None
    try:
        b64_str = data
        if "," in b64_str:
            b64_str = b64_str.split(",", 1)[1]
        raw_bytes = base64.b64decode(b64_str)
        return Image.open(io.BytesIO(raw_bytes)).convert("RGB")
    except Exception as e:
        print("Error decoding base64 image:", e)
        return None


def create_feathered_mask(w: int, h: int, feather_px: int = 20) -> Image.Image:
    """Creates a smooth alpha mask with soft gaussian falloff for seamless patch blending."""
    mask = Image.new("L", (w, h), 0)
    draw = ImageDraw.Draw(mask)
    inset = min(feather_px, min(w, h) // 6)
    draw.rectangle([inset, inset, w - inset, h - inset], fill=255)
    return mask.filter(ImageFilter.GaussianBlur(radius=inset))


def detect_qr_bounding_box(control_image: Image.Image) -> tuple[int, int, int, int]:
    """Detects active non-gray QR region on the control canvas (default fallback: center)."""
    w, h = control_image.size
    gray = control_image.convert("L")
    pixels = gray.load()
    
    min_x, min_y, max_x, max_y = w, h, 0, 0
    found = False
    
    for y in range(h):
        for x in range(w):
            val = pixels[x, y]
            # Detect black (<80) or white (>180) modules from the neutral gray (128) canvas
            if val < 80 or val > 180:
                found = True
                if x < min_x: min_x = x
                if y < min_y: min_y = y
                if x > max_x: max_x = x
                if y > max_y: max_y = y
                
    if not found or max_x <= min_x or max_y <= min_y:
        return int(w * 0.25), int(h * 0.25), int(w * 0.75), int(h * 0.75)
        
    # Add slight padding for context
    pad = 12
    return max(0, min_x - pad), max(0, min_y - pad), min(w, max_x + pad), min(h, max_y + pad)


@gpu_worker
def generate(
    prompt: str,
    negative_prompt: str,
    qr_control_image: str,
    reference_image: str,
    conditioning_scale: float,
    reference_strength: float,
    seed: int,
    num_outputs: int,
    steps: int,
):
    num_outputs = max(1, min(int(num_outputs), 4))
    steps = max(15, min(int(steps), 35))
    conditioning_scale = max(0.4, min(float(conditioning_scale), 2.5))
    reference_strength = max(0.2, min(float(reference_strength), 0.95))
    seed = int(seed) if int(seed) >= 0 else random.randint(0, 2**31 - 1)

    # 1. Decode QR control image sent from Go backend
    control_image = decode_base64_image(qr_control_image)
    if control_image is None:
        raise gr.Error("Valid qr_control_image is required from Go backend")
    
    if control_image.size != (1024, 1024):
        control_image = control_image.resize((1024, 1024), Image.Resampling.LANCZOS)

    # 2. Decode reference style image if provided
    raw_ref_image = decode_base64_image(reference_image)
    init_image = None
    if raw_ref_image is not None:
        init_image = ImageOps.fit(raw_ref_image, (1024, 1024), Image.Resampling.LANCZOS)

    if not LOCAL_WORKER:
        pipe.to("cuda")
        img2img_pipe.to("cuda")

    generators = [
        torch.Generator(device="cuda" if torch.cuda.is_available() else "cpu").manual_seed(seed + i)
        for i in range(num_outputs)
    ]
    images = []

    for generator in generators:
        if init_image is not None:
            # Regional Patch Diffusion & Seamless Alpha Feather Blending
            # 1. Find exact coordinates of QR region on the canvas
            x0, y0, x1, y1 = detect_qr_bounding_box(control_image)
            pw = x1 - x0
            ph = y1 - y0
            
            # 2. Crop patch from reference image & matching QR control patch
            ref_patch = init_image.crop((x0, y0, x1, y1)).resize((768, 768), Image.Resampling.LANCZOS)
            ctrl_patch = control_image.crop((x0, y0, x1, y1)).resize((768, 768), Image.Resampling.NEAREST)
            
            # 3. Diffuse only the cropped patch with QR ControlNet
            patch_result = img2img_pipe(
                prompt=prompt,
                negative_prompt=negative_prompt,
                image=ref_patch,
                control_image=ctrl_patch,
                strength=reference_strength,
                num_inference_steps=steps,
                guidance_scale=7.5,
                controlnet_conditioning_scale=conditioning_scale,
                generator=generator,
            ).images[0]
            
            # 4. Scale diffused patch back to original patch dimensions
            diffused_patch_resized = patch_result.resize((pw, ph), Image.Resampling.LANCZOS)
            
            # 5. Create smooth feathered alpha mask for seamless edge blending
            feather_mask = create_feathered_mask(pw, ph, feather_px=18)
            
            # 6. Composite the blended patch BACK into the 100% untouched original image
            final_composite = init_image.copy()
            final_composite.paste(diffused_patch_resized, (x0, y0), feather_mask)
            
            images.append(final_composite)
        else:
            result = pipe(
                prompt=prompt,
                negative_prompt=negative_prompt,
                image=control_image,
                num_inference_steps=steps,
                guidance_scale=8.0,
                controlnet_conditioning_scale=conditioning_scale,
                generator=generator,
                width=1024,
                height=1024,
            )
            images.append(result.images[0])

    return images


with gr.Blocks(title="Lemas Art QR Worker") as demo:
    gr.Markdown("# Lemas Art QR · Regional Patch Diffusion & Seamless Composite Worker")
    prompt_input = gr.Textbox(label="Prompt")
    negative_input = gr.Textbox(label="Negative prompt")
    qr_ctrl_input = gr.Textbox(label="QR Control Image (Base64 from Go)", lines=3)
    ref_image_input = gr.Textbox(label="Reference Image (Optional Base64)", lines=3)
    with gr.Row():
        scale_input = gr.Slider(0.4, 2.5, value=1.25, step=0.05, label="Conditioning Scale")
        strength_input = gr.Slider(0.2, 0.95, value=0.68, step=0.05, label="Patch Strength")
        seed_input = gr.Number(value=-1, precision=0, label="Seed")
        count_input = gr.Slider(1, 4, value=1, step=1, label="Outputs")
        steps_input = gr.Slider(15, 35, value=25, step=1, label="Steps")
    output = gr.Gallery(label="Generated candidates", columns=2)
    gr.Button("Generate", variant="primary").click(
        generate,
        inputs=[
            prompt_input,
            negative_input,
            qr_ctrl_input,
            ref_image_input,
            scale_input,
            strength_input,
            seed_input,
            count_input,
            steps_input,
        ],
        outputs=output,
        api_name="generate",
    )


if __name__ == "__main__":
    demo.queue(default_concurrency_limit=1 if LOCAL_WORKER else 2).launch(
        server_name="127.0.0.1" if LOCAL_WORKER else "0.0.0.0",
        server_port=7860,
        share=False,
    )
