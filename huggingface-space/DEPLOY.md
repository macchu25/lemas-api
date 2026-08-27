# Deploy Lemas Art QR to Hugging Face ZeroGPU

1. Create a new **Gradio Space** named `lemas-art-qr` in your Hugging Face account.
2. Copy `README.md`, `app.py`, and `requirements.txt` from this directory to the Space repository root.
3. Open **Settings → Hardware** and select **ZeroGPU**.
4. Wait until the Space status is **Running** and test the `generate` endpoint from **Use via API**.
5. If the Space is private, create a read token at `https://huggingface.co/settings/tokens`.
6. Configure Railway backend:

```env
HF_TOKEN=hf_xxxxxxxxx
HF_ART_QR_SPACE_URL=https://YOUR-ACCOUNT-lemas-art-qr.hf.space
ART_QR_MAX_ATTEMPTS=2
```

7. Redeploy Railway. The frontend needs no Hugging Face token and must continue pointing to the Lemas Go API.

## API contract

The Space exposes the named Gradio endpoint `generate` with these inputs in order:

1. QR source image
2. prompt
3. negative prompt
4. QR conditioning scale
5. seed
6. output count
7. inference steps

Do not rename the endpoint or reorder its inputs without updating `handlers/artqr.go`.
