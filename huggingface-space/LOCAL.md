# Local ControlNet worker (Windows NVIDIA)

Use Python 3.12 and an isolated `.artqr-venv` in the workspace parent of `server`.
Install CUDA PyTorch first:

```powershell
.\.artqr-venv\Scripts\python.exe -m pip install torch==2.6.0 --index-url https://download.pytorch.org/whl/cu124
.\.artqr-venv\Scripts\python.exe -m pip install -r server/huggingface-space/requirements-local.txt -c server/huggingface-space/constraints-local.txt
powershell -ExecutionPolicy Bypass -File server/huggingface-space/start-local.ps1
```

The first launch downloads the SD 1.5 and QR Monster models to `.artqr-models`.
Local mode uses CPU offload and sequential generation for lower VRAM usage.
It binds only to `127.0.0.1:7860`; do not expose it publicly without authentication.

For a backend on the same host, set `HF_ART_QR_SPACE_URL=http://127.0.0.1:7860`
in its local environment, leave `HF_TOKEN` empty, and restart the backend.
Do not put this loopback address in a hosted production backend.
The API remains `/gradio_api/call/generate` with seven positional inputs.

GPU generation does not guarantee QR readability. Keep exact payload validation;
never mark rejected images as verified. Test a synthetic QR before real data.
