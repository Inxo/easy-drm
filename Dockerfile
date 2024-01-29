FROM golang:alpine AS build
RUN apk --no-cache add gcc g++ make git
WORKDIR /go/src/app
COPY ./src ./src
COPY ./go.* ./
COPY ./Makefile ./
#COPY ./.env ./
RUN apk --no-cache add tzdata
ENV TZ Europe/Helsinki
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
RUN go mod tidy
RUN go mod vendor
RUN make build

FROM alpine
RUN apk --no-cache add ca-certificates
RUN apk --no-cache add tzdata logrotate
ENV TZ Europe/Helsinki
RUN ln -snf /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
WORKDIR /app
COPY --from=build /go/src/app/build /app/bin
COPY ./config/logrotate.d/app.log /etc/conf.d/logrotate
RUN chmod 0644 /etc/conf.d/logrotate
RUN chown root:root /etc/conf.d/logrotate
EXPOSE 8080
ENTRYPOINT /app/bin/stream start