import random
import math

import gradio as gr
import qrcode
import spaces
import torch
from diffusers import ControlNetModel, StableDiffusionControlNetPipeline, UniPCMultistepScheduler
from PIL import Image


BASE_MODEL = "stable-diffusion-v1-5/stable-diffusion-v1-5"
CONTROLNET_MODEL = "monster-labs/control_v1p_sd15_qrcode_monster"

controlnet = ControlNetModel.from_pretrained(
    CONTROLNET_MODEL,
    subfolder="v2",
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


def build_qr_condition(payload: str) -> tuple[Image.Image, Image.Image]:
    qr = qrcode.QRCode(
        error_correction=qrcode.constants.ERROR_CORRECT_H,
        box_size=1,
        border=4,
    )
    qr.add_data(payload, optimize=0)
    qr.make(fit=True)
    modules = len(qr.get_matrix())
    module_size = 16 if modules * 16 <= 896 else 12 if modules * 12 <= 896 else 8
    qr.box_size = module_size
    qr_image = qr.make_image(fill_color="black", back_color="white").convert("RGB")
    canvas_size = max(512, math.ceil((qr_image.width + 8 * module_size) / 256) * 256)
    if canvas_size > 1024:
        raise gr.Error("QR payload is too dense for artistic generation")
    offset = ((canvas_size - qr_image.width) // 2 // module_size) * module_size
    condition = Image.new("RGB", (canvas_size, canvas_size), (128, 128, 128))
    condition.paste(qr_image, (offset, offset))
    region = Image.new("L", condition.size, 0)
    region.paste(Image.new("L", qr_image.size, 255), (offset, offset))
    return condition, region


def correct_modules(image: Image.Image, condition: Image.Image, region: Image.Image, amount: float) -> Image.Image:
    image = image.convert("RGB")
    corrected = Image.blend(image, condition, amount)
    return Image.composite(corrected, image, region)


@spaces.GPU(duration=120)
def generate(
    payload: str,
    prompt: str,
    negative_prompt: str,
    conditioning_scale: float,
    seed: int,
    num_outputs: int,
    steps: int,
):
    if not payload:
        raise gr.Error("QR payload is required")
    num_outputs = max(1, min(int(num_outputs), 4))
    steps = max(15, min(int(steps), 35))
    conditioning_scale = max(0.8, min(float(conditioning_scale), 2.2))
    seed = int(seed) if int(seed) >= 0 else random.randint(0, 2**31 - 1)
    control_image, qr_region = build_qr_condition(payload)
    pipe.to("cuda")
    generators = [torch.Generator(device="cuda").manual_seed(seed + i) for i in range(num_outputs)]
    images = []
    correction_levels = [0.12, 0.22, 0.34, 0.48]
    for index, generator in enumerate(generators):
        result = pipe(
            prompt=prompt,
            negative_prompt=negative_prompt,
            image=control_image,
            num_inference_steps=steps,
            guidance_scale=8.5,
            controlnet_conditioning_scale=conditioning_scale,
            generator=generator,
            width=control_image.width,
            height=control_image.height,
        )
        amount = correction_levels[min(index, len(correction_levels) - 1)]
        images.append(correct_modules(result.images[0], control_image, qr_region, amount))
    return images


with gr.Blocks(title="Lemas Art QR Worker") as demo:
    gr.Markdown("# Lemas Art QR · QR-ControlNet ZeroGPU")
    with gr.Row():
        payload_input = gr.Textbox(label="Decoded QR payload", lines=4)
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
        inputs=[payload_input, prompt_input, negative_input, scale_input, seed_input, count_input, steps_input],
        outputs=output,
        api_name="generate",
    )


if __name__ == "__main__":
    demo.queue(default_concurrency_limit=2).launch()
