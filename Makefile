.PHONY: install journal release

install:
	echo "Stoping the meraki_endpoint service" ;\
	sudo systemctl stop meraki_endpoint.service ;\
	echo "Building the new version of meraki_endpoint" ;\
	go build ;\
	echo "Recreating the service" ;\
	sudo useradd meraki_endpoint -s /sbin/nologin -M ;\
	sudo cp ./meraki_endpoint.service /lib/systemd/system/. ;\
	sudo chmod 755 /lib/systemd/system/meraki_endpoint.service ;\
	sudo cp ./meraki_endpoint /usr/bin/meraki_endpoint ;\
	sudo mkdir -p /srv/meraki_endpoint ;\
	echo "Restarting service" ;\
	sudo systemctl enable meraki_endpoint.service ;\
	sudo systemctl start meraki_endpoint.service

journal:
	sudo journalctl -f -u meraki_endpoint

release:
	hub release create -a ./meraki_endpoint -m "Added the ability to turn on a pprof server" $$VERSION
