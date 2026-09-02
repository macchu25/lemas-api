"""Exercise the local worker with non-sensitive synthetic QR data."""
import io
import time
import requests
from PIL import Image
import zxingcpp

base = "http://127.0.0.1:7860"
payload = "https://example.com/lemas-local-test"
started = time.monotonic()
response = requests.post(base + "/gradio_api/call/generate", json={"data": [
    payload, "watercolor botanical leaves, deep green and cream, detailed painting",
    "blurry, text, watermark", 1.8, 12345, 1, 15,
]}, timeout=30)
response.raise_for_status()
event_id = response.json()["event_id"]
with requests.get(base + "/gradio_api/call/generate/" + event_id, stream=True, timeout=600) as stream:
    stream.raise_for_status()
    event = ""
    for line in stream.iter_lines(decode_unicode=True):
        if line.startswith("event:"):
            event = line[6:].strip()
        elif line.startswith("data:") and event == "error":
            raise RuntimeError(line)
        elif line.startswith("data:") and event == "complete":
            import json
            result = json.loads(line[5:])
            url = result[0][0]["image"]["url"]
            if not url.startswith(base + "/"):
                raise RuntimeError("Unexpected output host")
            image_response = requests.get(url, timeout=30)
            image_response.raise_for_status()
            image = Image.open(io.BytesIO(image_response.content))
            decoded = zxingcpp.read_barcodes(image)
            print({"seconds": round(time.monotonic()-started, 1), "size": image.size,
                   "verified": any(item.text == payload for item in decoded)}, flush=True)
            break
    else:
        raise RuntimeError("No completion event")
