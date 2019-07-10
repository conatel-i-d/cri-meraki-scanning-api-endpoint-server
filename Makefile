.PHONY: install logs

install:
	echo "Stoping the meraki_endpoint service" ;\
	sudo systemctl stop meraki_endpoint.service ;\
	echo "Building the new version of meraki_endpoint" ;\
	go build ;\
	echo "Recreating the service" ;\
	sudo useradd meraki_endpoint -s /sbin/nologin -M ;\
	sudo cp ./meraki_endpoint.service /lib/systemd/system/. ;\
	sudo chmod 755 /lib/systemd/system/meraki_endpoint.service ;\
	sudo mv ./meraki_endpoint /usr/bin/meraki_endpoint ;\
	sudo mkdir -p /srv/meraki_endpoint ;\
	echo "Restarting service" ;\
	sudo systemctl enable meraki_endpoint.service ;\
	sudo systemctl start meraki_endpoint.service

logs:
	sudo journalctl -f -u meraki_endpoint
