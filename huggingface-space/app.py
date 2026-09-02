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
from PIL import Image, ImageOps


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

# Shared memory img2img pipeline for style reference harmonization
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
    
    # Standardize to 1024x1024
    if control_image.size != (1024, 1024):
        control_image = control_image.resize((1024, 1024), Image.Resampling.LANCZOS)

    # 2. Decode reference style image if provided
    init_image = decode_base64_image(reference_image)
    if init_image is not None and init_image.size != (1024, 1024):
        init_image = ImageOps.fit(init_image, (1024, 1024), Image.Resampling.LANCZOS)

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
            # Single-pass full-canvas diffusion conditioned by reference image and full-canvas QR control
            result = img2img_pipe(
                prompt=prompt,
                negative_prompt=negative_prompt,
                image=init_image,
                control_image=control_image,
                strength=reference_strength,
                num_inference_steps=steps,
                guidance_scale=7.5,
                controlnet_conditioning_scale=conditioning_scale,
                generator=generator,
            )
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

        # Raw diffusion candidate - no module painting overlay
        images.append(result.images[0])
    return images


with gr.Blocks(title="Lemas Art QR Worker") as demo:
    gr.Markdown("# Lemas Art QR · QR-ControlNet Worker")
    prompt_input = gr.Textbox(label="Prompt")
    negative_input = gr.Textbox(label="Negative prompt")
    qr_ctrl_input = gr.Textbox(label="QR Control Image (Base64 from Go)", lines=3)
    ref_image_input = gr.Textbox(label="Reference Image (Optional Base64)", lines=3)
    with gr.Row():
        scale_input = gr.Slider(0.4, 2.5, value=1.0, step=0.05, label="Conditioning Scale")
        strength_input = gr.Slider(0.2, 0.95, value=0.45, step=0.05, label="Reference Strength")
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
