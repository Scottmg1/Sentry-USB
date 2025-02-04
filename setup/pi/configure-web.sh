#!/bin/bash -eu

setup_progress "configuring nginx"

# delete existing nginx fstab entries
sed -i "/.*\/nginx tmpfs.*/d" /etc/fstab
# and recreate them
echo "tmpfs /var/log/nginx tmpfs nodev,nosuid 0 0" >> /etc/fstab
echo "tmpfs /var/lib/nginx tmpfs nodev,nosuid 0 0" >> /etc/fstab
# only needed for initial setup, since systemd will create these automatically after that
mkdir -p /var/log/nginx
mkdir -p /var/lib/nginx
mount /var/log/nginx
mount /var/lib/nginx

apt-get -y --force-yes install nginx fcgiwrap libnginx-mod-http-fancyindex fuse libfuse-dev g++ net-tools wireless-tools ethtool

# install data files and config files
systemctl stop nginx.service &> /dev/null || true
mkdir -p /var/www
umount /var/www/html/TeslaCam &> /dev/null || true
umount /var/www/html/fs/Music &> /dev/null || true
umount /var/www/html/fs/LightShow &> /dev/null || true
umount /var/www/html/fs/Boombox &> /dev/null || true
find /var/www/html -mount \( -type f -o -type l \) -print0 | xargs -0 rm
cp -r "$SOURCE_DIR/teslausb-www/html" /var/www/
ln -sf /teslausb/teslausb-headless-setup.log /var/www/html/
ln -sf /mutable/archiveloop.log /var/www/html/
ln -sf /tmp/diagnostics.txt /var/www/html/
mkdir -p /var/www/html/TeslaCam
cp -rf "$SOURCE_DIR/teslausb-www/teslausb.nginx" /etc/nginx/sites-available
ln -sf /etc/nginx/sites-available/teslausb.nginx /etc/nginx/sites-enabled/default

# Setup /etc/nginx/.htpasswd if user requested web auth, otherwise disable auth_basic
if [ -n "${WEB_USERNAME:-}" ] && [ -n "${WEB_PASSWORD:-}" ]
then
  apt-get -y --force-yes install apache2-utils
  htpasswd -bc /etc/nginx/.htpasswd "$WEB_USERNAME" "$WEB_PASSWORD"
  sed -i 's/auth_basic off/auth_basic "Restricted Content"/' /etc/nginx/sites-available/teslausb.nginx
else
  sed -i 's/auth_basic "Restricted Content"/auth_basic off/' /etc/nginx/sites-available/teslausb.nginx
fi

# install the fuse layer needed to work around an incompatibility
# between Chrome and Tesla's recordings
g++ -o /root/cttseraser -D_FILE_OFFSET_BITS=64 "$SOURCE_DIR/teslausb-www/cttseraser.cpp" -lstdc++ -lfuse

# install new UI (compiled js/css files)
curlwrapper -L -o /tmp/webui.tgz https://github.com/marcone/teslausb-webui/releases/latest/download/teslausb-ui.tgz
tar -C /var/www/html -xf /tmp/webui.tgz
if [ -d /var/www/html/new ] && ! [ -e /var/www/html/new/favicon.ico ]
then
  ln -s /var/www/html/favicon.ico /var/www/html/new/favicon.ico
fi


cat > /sbin/mount.ctts << EOF
#!/bin/bash -eu
/root/cttseraser "\$@" -o allow_other
EOF
chmod +x /sbin/mount.ctts

sed -i '/mount.ctts/d' /etc/fstab
echo "mount.ctts#/mutable/TeslaCam /var/www/html/TeslaCam fuse defaults,nofail,x-systemd.requires=/mutable 0 0" >> /etc/fstab
mkdir -p /mutable/TeslaCam

sed -i 's/#user_allow_other/user_allow_other/' /etc/fuse.conf

# to get diagnostics and perform other teslausb functionality,
# nginx needs to be able to sudo
echo 'www-data ALL=(ALL) NOPASSWD: ALL' > /etc/sudoers.d/010_www-data-nopasswd
chmod 440 /etc/sudoers.d/010_www-data-nopasswd

# allow multiple concurrent cgi calls
cat > /etc/default/fcgiwrap << EOF
DAEMON_OPTS="-c 4 -f"
EOF

if [ -e /backingfiles/music_disk.bin ] || [ -e /backingfiles/lightshow_disk.bin ] || [ -e /backingfiles/boombox_disk.bin ]
then
  mkdir -p /var/www/html/fs
  copy_script run/auto.www /root/bin
  echo "/var/www/html/fs  /root/bin/auto.www" > /etc/auto.master.d/www.autofs
  apt-get -y --force-yes install zip
fi

setup_progress "done configuring nginx"
