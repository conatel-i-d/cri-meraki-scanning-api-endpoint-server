# meraki_endpoint

Endpoint to use against Meraki's Scanning API.

It takes the information sent by Cisco's Scanning API and stores it on an S3 bucket. To try to handle the 500ms round trip time required by Cisco, processing the data, and sending it to S3 is done using a queue. This allows to send a response back to Cisco before the data is processed.

A case could be made regarding how the data is read from the request. As of now, we are decoding it as we read it and passing the decoded interface to the queue. Shouldn't it be better just to read the JSON data into a list of bytes (`[]byte`). I measured both implementations and couldn't see a difference in response times. This could be changed in the future if new evidence arises.

Provides 3 endpoints to fulfill the API requirements:

- `GET /`: Returns the Meraki validator.
- `POST /`: Handles the reception of data by the API.
- `GET /healthz`: Health enpoint to use when presenting the service through an Application Load Balancer.

The service can be run standalone or behind an ALB. Meraki requires that the endpoints are served through SSL, so if you plan to run it in standalone mode, you should configure the `ssl`, `server-key`, and `server-crt` options. You should also configure the `port` option to 443. Remember to give the binaries the capabilities to listen on ports below 1024. On ubuntu you cand do something like this:

```bash
sudo apt-get install libcap2-bin -y
sudo setcap 'cap_net_bind_service=+ep' /your/path/gobinary
```

_[Source](https://fabianlee.org/2017/05/21/golang-running-a-go-binary-as-a-systemd-service-on-ubuntu-16-04/)_

You need valid certificates. If you don't own one, try using [Let's Encrypt](https://letsencrypt.org).

## Installation

### Run the binary

Download the binary from the releases site:

[https://github.com/guzmonne/meraki_endpoint/releases](https://github.com/guzmonne/meraki_endpoint/releases)

Give the app execution permissions and run it with the appropiate options

```bash
cd /tmp
export VERSION=0.0.3
wget https://github.com/guzmonne/meraki_endpoint/releases/download/$VERSION/meraki_endpoint
chmod +x meraki_endpoint
./meraki_endpoint \
  --port="8080" \
  --validator="das..." \
  --secret="s3cr3t" \
  --bucket="your.s3.bucket.com"
```

You can see all the app options by running `meraki_endpoint --help`.

### Run as a service

_This steps have only been tested on **Ubuntu Server 18.04**._

Clone the repo to the server where the service should run. Then, edit the `meraki_endpoint.service` and configure the options that match your setup (meraki secret, meraki validator, s3 bucket, ssl, etc.) You must configure this options by modifying the value of the `ExecStart` key.

```service
# ...

WorkingDirectory=/srv/meraki_endpoint
# Configure the necessary options to run the service on your setup
# ExecStart=/usr/bin/meraki_endpoint --port="8080" --bucket="some.bucket" --secret="s3cr3t" --validator="d283..."
# To use it in standalone mode serving http, remember to configure the ssl ans server options besides the other options
# ExecStart=/usr/bin/meraki_endpoint --port="443" --server-crt="/path/to/crt" --server-key="path/to/key"
ExecStart=/usr/bin/meraki_endpoint

# ...
```

After editing the service file, give execute permissions to the `./install.sh` script and run it. You can select a specific version of the service to install by modifying the `MERAKI_ENDPOINT_VERSION` variable before calling the script.

```bash
chmod +x ./install.sh
# To install the stable version
./install.sh
# To install a custom version
MERAKI_ENDPOINT_VERSION=0.0.3 ./install.sh
```

If everything goes well, the `meraki_endpoint` service will be enabled and running. You can check the logs using `journalctl`.

```bash
sudo journalctl -f -u meraki_endpoint
```

## Build

Clone the project into your `$GOPATH` folder and run `go build`.

## Licence

MIT

## Contributions

Welcomed. Create a PR and send it. If it looks good, it will be added to the repository.

## Author

Guzmán Monné <<godevops@outlook.com>>

