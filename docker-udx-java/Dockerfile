FROM phusion/baseimage:focal-1.2.0 as downloader

CMD ["/sbin/my_init"]

WORKDIR /tmp/

# ---- Option 1: download from web

RUN set -x \
&& apt-get update \
&& apt-get install wget \
&& wget https://download.java.net/openjdk/jdk8u41/ri/openjdk-8u41-b04-linux-x64-14_jan_2020.tar.gz \
&& tar xzvf openjdk-8u41-b04-linux-x64-14_jan_2020.tar.gz

# ---- Option 2: copy from local

# COPY ./java-se-8u41-ri /tmp/

##############################################################################################
FROM alpine:latest

COPY --from=downloader /tmp/java-se-8u41-ri/ /tmp/java-se-8u41-ri/

CMD ["/bin/sh"]