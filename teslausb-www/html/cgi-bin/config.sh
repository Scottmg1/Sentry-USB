#!/bin/bash

function exists(){
  if [ -e "$1" ]
  then
    echo -n yes
  else
    echo -n no
  fi
}

function configured(){
  if sudo grep -q "^export $1=" /root/teslausb_setup_variables.conf
  then
    echo -n yes
  else
    echo -n no
  fi
}

cat << EOF
HTTP/1.0 200 OK
Content-type: application/json

{
   "has_cam" : "$(exists /backingfiles/cam_disk.bin)",
   "has_music" : "$(exists /backingfiles/music_disk.bin)",
   "has_lightshow" : "$(exists /backingfiles/lightshow_disk.bin)",
   "has_boombox" : "$(exists /backingfiles/boombox_disk.bin)",
   "has_wraps" : "$(exists /backingfiles/wraps_disk.bin)",
   "uses_ble" : "$(configured TESLA_BLE_VIN)"
}
EOF
