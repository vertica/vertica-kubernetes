FROM phusion/baseimage:focal-1.2.0 as downloader

CMD ["/sbin/my_init"]

WORKDIR /tmp/

# ---- Option 1: download from web

# RUN set -x \
# && apt install wget \
# && wget https://download.java.net/openjdk/jdk8u41/ri/openjdk-8u41-b04-linux-x64-14_jan_2020.tar.gz

# ---- Option 2: copy from local

COPY ./java-se-8u41-ri /tmp/

RUN apt-get clean && rm -rf /var/lib/apt/lists/*