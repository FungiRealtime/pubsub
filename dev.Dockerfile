FROM golang:1.16

WORKDIR /app 

RUN curl -fLo install.sh https://raw.githubusercontent.com/cosmtrek/air/master/install.sh \
  && chmod +x install.sh && sh install.sh && cp ./bin/air /bin/air

# The image's build context should be the root of the project so we take this
# into consideration when accessing files.
CMD air -c ./.air.toml