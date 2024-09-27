You have to download Vertica RPM package before building docker image (even if you are
running on Ubuntu or Debian, the contents of the container use a Rocky Linux environment).

Download either [Community Edition](https://www.vertica.com/try/)(registration required)
or you can download Enterprise edition, if you are Vertica customer.

Store RPM packages into this folder, and remember to tell make the
name of the RPM when you build the image.

