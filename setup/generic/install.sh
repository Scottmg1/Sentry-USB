#!/bin/bash -eu
#
# Pre-install script to make things look sufficiently like what
# the main Raspberry Pi centric install scripts expect.
#

if [[ $EUID -ne 0 ]]
then
  echo "STOP: Run sudo -i."
  exit 1
fi

if [ ! -L /teslausb ]
then
  rm -rf /teslausb
  if [ -d /boot/firmware ] && findmnt --fstab /boot/firmware &> /dev/null
  then
    ln -s /boot/firmware /teslausb
  else
    ln -s /boot /teslausb
  fi
fi

function error_exit {
  echo "STOP: $*"
  exit 1
}

function flash_rapidly {
  for led in /sys/class/leds/*
  do 
    if [ -e "$led/trigger" ]
    then
      if ! grep -q timer "$led/trigger"
      then
        modprobe ledtrig-timer || echo "timer LED trigger unavailable"
      fi
      echo timer > "$led/trigger" || true
      if [ -e "$led/delay_off" ]
      then
        echo 150 > "$led/delay_off" || true
        echo 50 > "$led/delay_on" || true
      fi
    fi
  done
}

rootpart=$(findmnt -n -o SOURCE /)
rootname=$(lsblk -no pkname "${rootpart}")
rootdev="/dev/${rootname}"
marker="/root/RESIZE_ATTEMPTED"

# Check that the root partition is the last one.
lastpart=$(sfdisk -q -l "$rootdev" | tail +2 | sort -n -k 2 | tail -1 | awk '{print $1}')

# Check if there is sufficient unpartitioned space after the last
# partition to create the backingfiles and mutable partitions.
unpart=$(sfdisk -F "$rootdev" | grep -o '[0-9]* bytes' | head -1 | awk '{print $1}')
if [ "${1:-}" != "norootshrink" ] && [ "$unpart" -lt  $(( (1<<30) * 32)) ]
then
  # This script will only shrink the root partition, and if there's another
  # partition following the root partition, we won't be able to grow the
  # unpartitioned space at the end of the disk by shrinking the root partition.
  if [ "$rootpart" != "$lastpart" ]
  then
    error_exit "Insufficient unpartioned space, and root partition is not the last partition."
  fi

  # There is insufficient unpartitioned space.
  # Check if we've already shrunk the root filesystem, and shrink the root
  # partition to match if it hasn't been already

  devsectorsize=$(cat "/sys/block/${rootname}/queue/hw_sector_size")
  read -r fsblockcount fsblocksize < <(tune2fs -l "${rootpart}" | grep "Block count:\|Block size:" | awk ' {print $2}' FS=: | tr -d ' ' | tr '\n' ' ' | (cat; echo))
  fsnumsectors=$((fsblockcount * fsblocksize / devsectorsize))
  partnumsectors=$(sfdisk -q -l -o Sectors "${rootdev}" | tail +2 | sort -n | tail -1)
  partnumsectors=$((partnumsectors - 1));
  if [ "$partnumsectors" -le "$fsnumsectors" ]
  then
    if [ -f "$marker" ]
    then
      error_exit "Previous resize attempt failed. Delete $marker before retrying."
    fi
    touch "$marker"

    echo "insufficient unpartitioned space, attempting to shrink root file system"

    cat <<- EOF > /etc/rc.local
		#!/bin/bash
		{
		  while ! curl -s https://raw.githubusercontent.com/marcone/teslausb/main-dev/setup/generic/install.sh
		  do
		    sleep 1
		  done
		} | bash
		EOF
    chmod a+x /etc/rc.local

    if [ ! -e "/boot/initrd.img-$(uname -r)" ]
    then
      # This device did not boot using an initramfs. If we're running
      # Raspberry Pi OS, we can switch it over to using initramfs first,
      # then revert back after.
      if [ -f /etc/os-release ] && grep -q Raspbian /etc/os-release && [ -e /teslausb/config.txt ]
      then
        echo "Temporarily switching Rasspberry Pi OS to use initramfs"
        update-initramfs -c -k "$(uname -r)"
        echo "initramfs initrd.img-$(uname -r) followkernel # TESLAUSB-REMOVE" >> /teslausb/config.txt
      else
        error_exit "can't automatically shrink root partition for this OS, please shrink it manually before proceeding"
      fi
    fi

    {
      while ! curl -s https://raw.githubusercontent.com/marcone/teslausb/main-dev/tools/debian-resizefs.sh
      do
        sleep 1
      done
    } | bash -s 3G
    exit 0
  fi
  rm -f "$marker"
  # shrink root partition to match root file system size
  echo "shrinking root partition to match root fs, $fsnumsectors sectors"
  sleep 3
  rootpartstartsector=$(sfdisk -q -l -o Start "${rootdev}" | tail +2 | sort -n | tail -1)
  partnum=${rootpart:0-1}

  echo "${rootpartstartsector},${fsnumsectors}" | sfdisk --force "${rootdev}" -N "${partnum}"

  if [ -e /teslausb/config.txt ] && grep -q TESLAUSB-REMOVE /teslausb/config.txt
  then
    # switch Raspberry Pi OS back to not using initramfs
    sed -i '/TESLAUSB-REMOVE/d' /teslausb/config.txt
    rm -rf "/boot/initrd.img-$(uname -r)"
  else
    # restore initramfs without the resize code that debian-resizefs.sh added
    update-initramfs -u
  fi

  reboot
  exit 0
fi

# Copy the sample config file from github
if [ ! -e /teslausb/teslausb_setup_variables.conf ] && [ ! -e /root/teslausb_setup_variables.conf ]
then
  while ! curl -o /teslausb/teslausb_setup_variables.conf https://raw.githubusercontent.com/marcone/teslausb/main-dev/pi-gen-sources/00-teslausb-tweaks/files/teslausb_setup_variables.conf.sample
  do
    sleep 1
  done
fi

# and the wifi config template
if [ ! -e /teslausb/wpa_supplicant.conf.sample ]
then
  while ! curl -o /teslausb/wpa_supplicant.conf.sample https://raw.githubusercontent.com/marcone/teslausb/main-dev/pi-gen-sources/00-teslausb-tweaks/files/wpa_supplicant.conf.sample
  do
    sleep 1
  done
fi

# The user should have configured networking manually, so disable wifi setup
touch /teslausb/WIFI_ENABLED

# Copy our rc.local from github, which will allow setup to
# continue using the regular "one step setup" process used
# for setting up a Raspberry Pi with the prebuilt image
rm -f /etc/rc.local
while ! curl -o /etc/rc.local https://raw.githubusercontent.com/marcone/teslausb/main-dev/pi-gen-sources/00-teslausb-tweaks/files/rc.local
do
  sleep 1
done
chmod a+x /etc/rc.local

if [ ! -x "$(command -v dos2unix)" ]
then
  apt install -y dos2unix
fi

if [ ! -x "$(command -v sntp)" ] && [ ! -x "$(command -v ntpdig)" ]
then
  apt install -y sntp || apt install -y ntpsec-ntpdig
fi

if [ ! -x "$(command -v parted)" ]
then
  apt install -y parted
fi

if [ ! -x "$(command -v fdisk)" ]
then
  apt install -y fdisk
fi


# indicate we're waiting for the user to log in and finish setup
flash_rapidly

# If there is a user with id 1000, assume it is the default user
# the user will be logging in as.
DEFUSER=$(grep ":1000:1000:" /etc/passwd | awk -F : '{print $1}')
if [ -n "$DEFUSER" ]
then
  if [ ! -e "/home/$DEFUSER/.bashrc" ] || ! grep -q "SETUP_FINISHED" "/home/$DEFUSER/.bashrc"
  then
    cat <<- EOF >> "/home/$DEFUSER/.bashrc"
		if [ ! -e /teslausb/TESLAUSB_SETUP_FINISHED ]
		then
		  echo "+-------------------------------------------+"
		  echo "| To continue teslausb setup, run 'sudo -i' |"
		  echo "+-------------------------------------------+"
		fi
	EOF
    chown "$DEFUSER:$DEFUSER" "/home/$DEFUSER/.bashrc"
  fi
fi

if ! grep -q "SETUP_FINISHED" /root/.bashrc
then
  cat <<- EOF >> /root/.bashrc
	if [ ! -e /teslausb/TESLAUSB_SETUP_FINISHED ]
	then
	  echo "+------------------------------------------------------------------------+"
	  echo "| To continue teslausb setup, edit the file                              |"
	  echo "| /teslausb/teslausb_setup_variables.conf with your favorite             |"
	  echo "| editor, e.g. 'nano /teslausb/teslausb_setup_variables.conf' and fill   |"
	  echo "| in the required variables. Instructions are in the file, and at        |"
	  echo "| https://github.com/marcone/teslausb/blob/main-dev/doc/OneStepSetup.md  |"
	  echo "| (though ignore the Raspberry Pi specific bits about flashing and       |"
	  echo "| mounting the sd card on a PC)                                          |"
	  echo "|                                                                        |"
	  echo "| When done, save changes and run /etc/rc.local                          |"
	  echo "+------------------------------------------------------------------------+"
	fi
	EOF
fi

# hack to print the above message without duplicating it here
grep -A 12 SETUP_FINISHED .bashrc  | grep echo | while read line; do eval "$line"; done
