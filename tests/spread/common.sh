cleanup_manpages() {
    systemctl stop nginx || true
    systemctl stop fcgiwrap || true
    systemctl stop update-manpages.service || true

    rm -rf /app
    rm -rf /etc/systemd/system/update-manpages.service

    apt-get purge -y nginx-full fcgiwrap jq curl w3m
    systemctl daemon-reload
}
