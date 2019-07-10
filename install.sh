#!/bin/bash

MERAKI_ENDPOINT_VERSION=${MERAKI_ENDPOINT_VERSION:-"0.0.2"}

echo "Creating user meraki_endpoint"
sudo useradd meraki_endpoint -s /sbin/nologin -M
echo "Moving service configuration to /lib/systemd/system/"
sudo mv ./meraki_endpoint.service /lib/systemd/system/.
sudo chmod 755 /lib/systemd/system/meraki_endpoint.service
echo "Downloading meraki_endpoint binaries"
wget --quiet https://github.com/guzmonne/meraki_endpoint/releases/download/$MERAKI_ENDPOINT_VERSION/meraki_endpoint
sudo mv ./meraki_endpoint /usr/bin/meraki_endpoint
echo "Creating application folders"
sudo mkdir -p /srv/meraki_endpoint
echo "Enable and start service"
sudo systemctl enable meraki_endpoint.service 
sudo systemctl start meraki_endpoint.service 