FROM nvidia/cuda:12.4.1-base-ubuntu22.04

ENV DEBIAN_FRONTEND=noninteractive \
    PYTHONUNBUFFERED=1 \
    MPLBACKEND=Agg

RUN sed -i 's@//.*archive.ubuntu.com@//mirrors.tuna.tsinghua.edu.cn@g; s@//.*security.ubuntu.com@//mirrors.tuna.tsinghua.edu.cn@g' /etc/apt/sources.list \
    && apt-get update && apt-get install -y --no-install-recommends \
        python3 \
        python3-pip \
        ffmpeg \
        libgl1 \
        libglib2.0-0 \
        fonts-noto-cjk \
        git \
    && rm -rf /var/lib/apt/lists/*

RUN pip3 install --no-cache-dir --default-timeout=120 \
        -i https://pypi.tuna.tsinghua.edu.cn/simple \
        torch==2.5.1 \
        torchvision==0.20.1 \
        opencv-python-headless==4.10.0.84 \
        numpy==1.26.4 \
        scenedetect==0.6.4 \
        easyocr==1.7.2 \
        scikit-image==0.24.0 \
        av==13.1.0 \
        tqdm==4.66.5 \
        imageio==2.35.1 \
        imageio-ffmpeg==0.5.1 \
        einops==0.8.0 \
        pyyaml==6.0.2 \
        addict==2.4.0 \
        future==1.0.0 \
        scipy==1.14.1 \
        matplotlib==3.9.2 \
        requests==2.32.3 \
        timm==1.0.9

COPY weights/craft_mlt_25k.zip weights/zh_sim_g2.zip weights/english_g2.zip /tmp/
RUN mkdir -p /root/.EasyOCR/model \
    && python3 -c "\
import zipfile, glob; \
[zipfile.ZipFile(z).extractall('/root/.EasyOCR/model') for z in glob.glob('/tmp/*.zip')]" \
    && rm /tmp/*.zip \
    && ls -la /root/.EasyOCR/model

WORKDIR /work
