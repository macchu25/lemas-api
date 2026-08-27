import random

import gradio as gr
import spaces
import torch
from diffusers import ControlNetModel, StableDiffusionControlNetPipeline, UniPCMultistepScheduler
from PIL import Image, ImageEnhance, ImageOps


BASE_MODEL = "stable-diffusion-v1-5/stable-diffusion-v1-5"
CONTROLNET_MODEL = "DionTimmer/controlnet_qrcode-control_v1p_sd15"

controlnet = ControlNetModel.from_pretrained(
    CONTROLNET_MODEL,
    torch_dtype=torch.float16,
)
pipe = StableDiffusionControlNetPipeline.from_pretrained(
    BASE_MODEL,
    controlnet=controlnet,
    torch_dtype=torch.float16,
    safety_checker=None,
    requires_safety_checker=False,
)
pipe.scheduler = UniPCMultistepScheduler.from_config(pipe.scheduler.config)
pipe.enable_attention_slicing()


def prepare_qr(image: Image.Image) -> Image.Image:
    image = ImageOps.exif_transpose(image).convert("RGB")
    image = ImageOps.fit(image, (768, 768), method=Image.Resampling.NEAREST)
    image = ImageEnhance.Contrast(image).enhance(1.25)
    return image


@spaces.GPU(duration=120)
def generate(
    qr_image: Image.Image,
    prompt: str,
    negative_prompt: str,
    conditioning_scale: float,
    seed: int,
    num_outputs: int,
    steps: int,
):
    if qr_image is None:
        raise gr.Error("QR image is required")
    num_outputs = max(1, min(int(num_outputs), 4))
    steps = max(15, min(int(steps), 35))
    conditioning_scale = max(0.8, min(float(conditioning_scale), 2.2))
    seed = int(seed) if int(seed) >= 0 else random.randint(0, 2**31 - 1)
    control_image = prepare_qr(qr_image)
    pipe.to("cuda")
    generators = [torch.Generator(device="cuda").manual_seed(seed + i) for i in range(num_outputs)]
    result = pipe(
        prompt=[prompt] * num_outputs,
        negative_prompt=[negative_prompt] * num_outputs,
        image=[control_image] * num_outputs,
        num_inference_steps=steps,
        guidance_scale=8.5,
        controlnet_conditioning_scale=conditioning_scale,
        generator=generators,
    )
    return result.images


with gr.Blocks(title="Lemas Art QR Worker") as demo:
    gr.Markdown("# Lemas Art QR · QR-ControlNet ZeroGPU")
    with gr.Row():
        qr_input = gr.Image(type="pil", label="QR source")
        output = gr.Gallery(label="Generated candidates", columns=2)
    prompt_input = gr.Textbox(label="Prompt")
    negative_input = gr.Textbox(label="Negative prompt")
    with gr.Row():
        scale_input = gr.Slider(0.8, 2.2, value=1.35, step=0.05, label="QR conditioning")
        seed_input = gr.Number(value=-1, precision=0, label="Seed")
        count_input = gr.Slider(1, 4, value=4, step=1, label="Outputs")
        steps_input = gr.Slider(15, 35, value=25, step=1, label="Steps")
    gr.Button("Generate", variant="primary").click(
        generate,
        inputs=[qr_input, prompt_input, negative_input, scale_input, seed_input, count_input, steps_input],
        outputs=output,
        api_name="generate",
    )


if __name__ == "__main__":
    demo.queue(default_concurrency_limit=2).launch()
