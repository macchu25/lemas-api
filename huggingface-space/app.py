import random
import math
import os

import gradio as gr
import qrcode
LOCAL_WORKER = os.getenv("ART_QR_LOCAL") == "1"
if not LOCAL_WORKER:
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
    use_safetensors=True,
)
pipe = StableDiffusionControlNetPipeline.from_pretrained(
    BASE_MODEL,
    controlnet=controlnet,
    torch_dtype=torch.float16,
    use_safetensors=True,
    variant="fp16" if LOCAL_WORKER else None,
    safety_checker=None,
    requires_safety_checker=False,
)
pipe.scheduler = UniPCMultistepScheduler.from_config(pipe.scheduler.config)
pipe.enable_attention_slicing()
if LOCAL_WORKER:
    pipe.enable_model_cpu_offload()
    pipe.enable_vae_slicing()


def gpu_worker(fn):
    return fn if LOCAL_WORKER else spaces.GPU(duration=120)(fn)


def build_qr_condition(payload: str) -> tuple[Image.Image, Image.Image, Image.Image]:
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
    functional = Image.new("L", condition.size, 0)

    def protect(left: int, top: int, width: int, height: int) -> None:
        functional.paste(
            255,
            (
                offset + left * module_size,
                offset + top * module_size,
                offset + (left + width) * module_size,
                offset + (top + height) * module_size,
            ),
        )

    border = qr.border
    symbol_modules = modules - 2 * border
    protect(border - 1, border - 1, 9, 9)
    protect(border + symbol_modules - 8, border - 1, 9, 9)
    protect(border - 1, border + symbol_modules - 8, 9, 9)
    protect(border, border + 6, symbol_modules, 1)
    protect(border + 6, border, 1, symbol_modules)
    protect(border, border + 8, 9, 1)
    protect(border + 8, border, 1, 9)

    for row in qrcode.util.pattern_position(qr.version):
        for column in qrcode.util.pattern_position(qr.version):
            if (
                (row < 9 and column < 9)
                or (row < 9 and column > symbol_modules - 9)
                or (row > symbol_modules - 9 and column < 9)
            ):
                continue
            protect(border + column - 2, border + row - 2, 5, 5)

    return condition, region, functional


def correct_modules(
    image: Image.Image,
    condition: Image.Image,
    region: Image.Image,
    functional: Image.Image,
    amount: float,
) -> Image.Image:
    image = image.convert("RGB")
    corrected = Image.blend(image, condition, amount)
    corrected = Image.composite(corrected, image, region)
    protected = Image.blend(corrected, condition, max(0.94, amount))
    return Image.composite(protected, corrected, functional)


@gpu_worker
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
    control_image, qr_region, functional_region = build_qr_condition(payload)
    if not LOCAL_WORKER:
        pipe.to("cuda")
    generators = [torch.Generator(device="cuda").manual_seed(seed + i) for i in range(num_outputs)]
    images = []
    correction_levels = [0.25, 0.45, 0.65, 0.85]
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
        images.append(
            correct_modules(
                result.images[0],
                control_image,
                qr_region,
                functional_region,
                amount,
            )
        )
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
    demo.queue(default_concurrency_limit=1 if LOCAL_WORKER else 2).launch(
        server_name="127.0.0.1" if LOCAL_WORKER else "0.0.0.0",
        server_port=7860,
        share=False,
    )
