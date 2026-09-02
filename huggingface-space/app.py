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


def create_feathered_mask(w: int, h: int, feather_px: int = 14) -> Image.Image:
    """Creates a smooth alpha mask with soft gaussian falloff for seamless patch blending."""
    mask = Image.new("L", (w, h), 0)
    draw = ImageDraw.Draw(mask)
    inset = min(feather_px, min(w, h) // 10)
    draw.rectangle([inset, inset, w - inset, h - inset], fill=255)
    return mask.filter(ImageFilter.GaussianBlur(radius=inset))


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
    placement_x: float = 0.25,
    placement_y: float = 0.25,
    placement_size: float = 0.5,
):
    num_outputs = max(1, min(int(num_outputs), 4))
    steps = max(15, min(int(steps), 35))
    conditioning_scale = max(0.8, min(float(conditioning_scale), 2.5))
    reference_strength = max(0.4, min(float(reference_strength), 0.95))
    seed = int(seed) if int(seed) >= 0 else random.randint(0, 2**31 - 1)

    # 1. Decode QR control image sent from Go backend
    control_image = decode_base64_image(qr_control_image)
    if control_image is None:
        raise gr.Error("Valid qr_control_image is required from Go backend")

    # 2. Decode reference style image (KEEP EXACT ORIGINAL ASPECT RATIO)
    raw_ref_image = decode_base64_image(reference_image)

    if not LOCAL_WORKER:
        pipe.to("cuda")
        img2img_pipe.to("cuda")

    generators = [
        torch.Generator(device="cuda" if torch.cuda.is_available() else "cpu").manual_seed(seed + i)
        for i in range(num_outputs)
    ]
    images = []

    for generator in generators:
        if raw_ref_image is not None:
            # 1. Preserve 100% of the original photo's dimensions (e.g. 768x1024 portrait)
            orig_w, orig_h = raw_ref_image.size
            min_dim = min(orig_w, orig_h)

            # 2. Calculate pixel coordinates on the native image
            px = float(placement_x)
            py = float(placement_y)
            ps = float(placement_size)
            if ps <= 0 or ps > 1.0: ps = 0.5
            if px < 0: px = 0
            if py < 0: py = 0

            p_size = int(ps * min_dim)
            if p_size < 128: p_size = min_dim // 2
            px0 = max(0, min(orig_w - p_size, int(px * orig_w)))
            py0 = max(0, min(orig_h - p_size, int(py * orig_h)))

            # 3. Crop patch from reference image
            ref_patch = raw_ref_image.crop((px0, py0, px0 + p_size, py0 + p_size)).resize((768, 768), Image.Resampling.LANCZOS)

            # 4. Crop matching QR control patch from control canvas
            cw, ch = control_image.size
            c_min_dim = min(cw, ch)
            c_psize = int(ps * c_min_dim)
            c_px0 = max(0, min(cw - c_psize, int(px * cw)))
            c_py0 = max(0, min(ch - c_psize, int(py * ch)))
            ctrl_patch = control_image.crop((c_px0, c_py0, c_px0 + c_psize, c_py0 + c_psize)).resize((768, 768), Image.Resampling.NEAREST)

            # 5. Diffuse ONLY that cropped patch with QR ControlNet
            patch_result = img2img_pipe(
                prompt=prompt,
                negative_prompt=negative_prompt,
                image=ref_patch,
                control_image=ctrl_patch,
                strength=reference_strength,
                num_inference_steps=steps,
                guidance_scale=8.0,
                controlnet_conditioning_scale=conditioning_scale,
                control_guidance_start=0.0,
                control_guidance_end=0.88,
                generator=generator,
            ).images[0]

            # 6. Scale diffused patch back to exact target dimensions
            diffused_patch_resized = patch_result.resize((p_size, p_size), Image.Resampling.LANCZOS)

            # 7. Create seamless alpha feather mask
            feather_mask = create_feathered_mask(p_size, p_size, feather_px=max(8, p_size // 18))

            # 8. Composite the blended patch BACK into the 100% UNTOUCHED native aspect-ratio photo
            final_composite = raw_ref_image.copy()
            final_composite.paste(diffused_patch_resized, (px0, py0), feather_mask)

            images.append(final_composite)
        else:
            if control_image.size != (1024, 1024):
                control_image = control_image.resize((1024, 1024), Image.Resampling.LANCZOS)
            result = pipe(
                prompt=prompt,
                negative_prompt=negative_prompt,
                image=control_image,
                num_inference_steps=steps,
                guidance_scale=8.0,
                controlnet_conditioning_scale=conditioning_scale,
                control_guidance_start=0.0,
                control_guidance_end=0.88,
                generator=generator,
                width=1024,
                height=1024,
            )
            images.append(result.images[0])

    return images


with gr.Blocks(title="Lemas Art QR Worker") as demo:
    gr.Markdown("# Lemas Art QR · Native Aspect Ratio Patch Diffusion Worker")
    prompt_input = gr.Textbox(label="Prompt")
    negative_input = gr.Textbox(label="Negative prompt")
    qr_ctrl_input = gr.Textbox(label="QR Control Image (Base64 from Go)", lines=3)
    ref_image_input = gr.Textbox(label="Reference Image (Optional Base64)", lines=3)
    with gr.Row():
        scale_input = gr.Slider(0.4, 2.5, value=1.35, step=0.05, label="Conditioning Scale")
        strength_input = gr.Slider(0.2, 0.95, value=0.72, step=0.05, label="Patch Strength")
        seed_input = gr.Number(value=-1, precision=0, label="Seed")
        count_input = gr.Slider(1, 4, value=1, step=1, label="Outputs")
        steps_input = gr.Slider(15, 35, value=25, step=1, label="Steps")
    with gr.Row():
        px_input = gr.Number(value=0.25, label="Placement X")
        py_input = gr.Number(value=0.25, label="Placement Y")
        psize_input = gr.Number(value=0.5, label="Placement Size")
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
            px_input,
            py_input,
            psize_input,
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
