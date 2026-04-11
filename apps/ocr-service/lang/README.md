# Tesseract Language Pack Installation

The OCR service requires Tesseract 5.x with Turkish (`tur`) and English (`eng`)
trained data.

## Standard Installation (Docker / Debian/Ubuntu)

These are installed automatically in the Dockerfile:

```bash
apt-get install -y tesseract-ocr tesseract-ocr-tur tesseract-ocr-eng
```

## Verify Installation

```bash
tesseract --version
tesseract --list-langs
```

Expected output includes `tur` and `eng` in the language list.

## Air-Gapped Environments

If the server has no internet access:

1. Download the `.deb` packages on a connected machine:
   ```bash
   apt-get download tesseract-ocr tesseract-ocr-tur tesseract-ocr-eng
   ```

2. Transfer to the target server and install:
   ```bash
   dpkg -i tesseract-ocr_*.deb tesseract-ocr-tur_*.deb tesseract-ocr-eng_*.deb
   ```

3. Alternatively, download the `.traineddata` files directly from the
   [Tesseract GitHub tessdata repo](https://github.com/tesseract-ocr/tessdata)
   and place them in `/usr/share/tesseract-ocr/5/tessdata/` (or wherever
   `TESSDATA_PREFIX` points).

## Environment Variable Override

Set `PERSONEL_OCR_TESSDATA_PREFIX` (or `TESSDATA_PREFIX`) to point to a
custom directory containing `.traineddata` files:

```bash
PERSONEL_OCR_TESSDATA_PREFIX=/opt/tessdata
```

## Language Quality Notes

- `tur` (Turkish): Tesseract 5.x LSTM engine performs well on clean
  screenshots of standard Turkish UI text. Quality degrades on stylised
  fonts or very low resolution.
- `eng` (English): Excellent quality for standard Latin script.
- For mixed-script images pass `languages: ["tr", "en"]` in the API request;
  the Tesseract engine will concatenate as `tur+eng`.

## PaddleOCR (Supplementary)

PaddleOCR is an optional fallback engine for handwriting and CJK script.
It is not required for the primary Turkish/English use case.
See `engines/paddle.py` for the integration details and Phase 3 roadmap.
