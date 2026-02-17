# One-step setup

This is a streamlined process for setting up the Pi. You'll flash a preconfigured version of Raspbian Buster Lite and then fill out a config file.

## Notes

- Assumes your Pi has access to Wifi, with internet access (during setup). (But all setup methods do currently.) USB networking is still enabled for troubleshooting or manual setup
- This image will work for either _headless_ (tested) or _manual_ (tested less) setup.
- Currently not tested with the rclone method when using headless setup, however you can specify 'none' as the archive method in the config file, which will configure the pi as a wifi-accessible USB drive, so you can then [configure rclone](./SetupRClone.md) or [configure rsync](./SetupRSync.md) and rerun the setup-sentryusb script.

## Configure the SD card before first boot of the Pi

1.  Flash the [latest image release](https://github.com/Scottmg1/Sentry-USB/releases/latest) using [Raspberry Pi Imager](https://www.raspberrypi.com/software/) or a similar flashing tool.

    In Raspberry Pi Imager, you need to click 'Operating System' and then scroll _all the way down_ and select the 'Use custom' option.

1.  Mount the card again, and in the `boot` directory create a `sentryusb.conf` file to export the same environment variables normally needed for manual setup (including archive info, Wifi, and push notifications (if desired).
    A sample conf file is located in the `boot` folder on the SD card. The latest sample is also available [from GitHub](https://github.com/Scottmg1/Sentry-USB/blob/main-dev/pi-gen-sources/00-sentryusb-tweaks/files/sentryusb.conf.sample).
    The sample file contains documentation and suggestions for values.

    > **Note** When creating/editing the configuration file on Windows, ensure that it is saved with the correct extension. It is recommended to disable the "hide extensions for known file types" option in Windows so you can see the full file name.

    Be sure that all values, especially your WiFi SSID and password are properly quoted and/or escaped according to [bash quoting rules](https://www.gnu.org/software/bash/manual/bash.html#Quoting), and that in addition any `&`, `/` and `\` are also escaped by prefixing them with a `\`.
    If the password does not contain a single quote character, you can enclose the entire password in single quotes, like so:

    ```
    export WIFIPASS='password'
    ```

    even if it contains other characters that might otherwise be special to bash, like \\, \* and $ (but note that the \\ should still be escaped with an additional \\ in order for the password to be correctly handled)

    If the password does contain a single quote, you will need to use a different syntax. E.g. if the password is `pass'word`, you would use:

    ```
    export WIFIPASS=$'pass\'word'
    ```

    and if the password contains both a single quote and a backslash, e.g. `pass'wo\rd`you'd use:

    ```
    export WIFIPASS=$'pass\'wo\\rd'
    ```

    Similarly if your WiFi SSID has spaces in its name, make sure they're escaped or quoted.

    For example, if your SSID were

    ```
    Foo Bar 2.4 GHz
    ```

    you would use

    ```
    export SSID=Foo\ Bar\ 2.4\ GHz
    ```

    or

    ```
    export SSID='Foo Bar 2.4 GHz'
    ```

1.  Boot it in your Pi, give it a few minutes, watching for a series of flashes (2, 3, 4, 5) and then a reboot and/or the CAM/music drives to become available on your PC/Mac. If you configured automatic music syncing, the drives won't be available on the PC/Mac until music syncing is complete. The LED flash stages during setup are:

    | Stage (number of flashes) | Activity                                                           |
    | ------------------------- | ------------------------------------------------------------------ |
    | 2                         | Verify the requested configuration is creatable                    |
    | 3                         | Grab scripts to start/continue setup                               |
    | 4                         | Create partition and files to store camera clips/music)            |
    | 5                         | Setup completed; remounting filesystems as read-only and rebooting |

The Pi should be available for `ssh` at `pi@sentryusb.local`, over Wifi (if automatic setup works) or USB networking (if it doesn't). It takes about 5 minutes, or more depending on network speed, etc. The default password for user `pi@sentryusb.local` is `raspberry`.

If plugged into just a power source, or your car, give it a few minutes until the LED starts pulsing steadily which means the archive loop is running and you're good to go.

You should see in `/sentryusb` the `SENTRYUSB_SETUP_FINISHED` and `WIFI_ENABLED` files as markers of headless setup success as well.

## Security

Given that the Pi contains sensitive information like your home wifi password and possible a Tesla account access token, please consider the following:

1. If WiFi Access Point is configured, ensure it is configured with a strong password. Make it something better than Passw0rd, more than 8 characters. The longer the password the better. See [here](https://en.wikipedia.org/wiki/Password_strength) or [here](https://xkcd.com/936/) for password strength.

2. Change the password for the pi account to something other than the default "raspberry". To do that, ssh into the Pi, run the following commands, and enter a new password when prompted:

```
   sudo -i
   /root/bin/remountfs_rw
   passwd pi
   reboot
```

3. Remember that the Pi contains a configuration file with sensitive information. If your Pi is stolen or you suspect an unauthorized person accessed it, immediately change your Tesla account password (if you configured the Pi to use your Tesla credentials to keep the car awake during archiving) and home wifi password.

### Troubleshooting

- If everything seems to be working, but you still don't see the USB drive(s) either on your local machine, or in the car, check that you are indeed using a USB data cable, and not a charge-only cable. Also ensure you are plugged into the USB port on the Raspberry PI, and not the power port.
- `ssh` to `pi@sentryusb.local` (assuming Wifi came up, or your Pi is connected to your computer via USB) and look at the `/sentryusb/sentryusb-setup.log`.
- Try `sudo -i` and then run `/etc/rc.local`. The scripts are fairly resilient to restarting and not re-running previous steps, and will tell you about progress/failure.
- If Wifi didn't come up:
  - Double-check the SSID and WIFIPASS variables in `sentryusb.conf`, and remove `WIFI_ENABLED`, then boot the SD in your Pi to retry automatic Wifi setup.
  - If you are using a WiFi network with a _hidden SSID_, edit `/boot/wpa_supplicant.conf.sample` and uncomment the line `scan_ssid=1` in the `network={...}` block.
  - If still no go, re-run `/etc/rc.local`
  - If all else fails, copy `/boot/wpa_supplicant.conf.sample` to `/boot/wpa_supplicant.conf` and edit out the `TEMP` variables to your desired settings.
- Note: if you get an error about `read-only filesystem`, you may have to `sudo -i` and run `/root/bin/remountfs_rw`.
- Try `date` to ensure the system clock is set correctly. If it is too far off, SSL/TLS Authentication will fail, preventing the installation from completing. You can set the date like `date -s "2 JAN 2022 15:04:05"`
- Try `tail -f /sentryusb/sentryusb-setup.log` to watch the logs during installation, which may shed some light on any errors occurring. Press `Ctrl-C` to stop watching logs.

More troubleshooting information in the [wiki](https://github.com/Scottmg1/Sentry-USB/wiki/Troubleshooting)

# Background information

## What happens under the covers

When the Pi boots the first time:

- A `/sentryusb/sentryusb-setup.log` file will be created and stages logged.
- Marker files will be created in `/sentryusb` like `SENTRYUSB_SETUP_STARTED` and `SENTRYUSB_SETUP_FINISHED` to track progress.
- Wifi is detected by looking for `/sentryusb/WIFI_ENABLED` and if not, creates the `wpa_supplicant.conf` file in place, using `SSID` and `WIFIPASS` from `sentryusb.conf` and reboots.
- The Pi LED will flash patterns (2, 3, 4, 5) as it gets to each stage (labeled in the setup-sentryusb script).
- After the final stage and reboot the LED will go back to normal. Remember, the step to remount the filesystem takes a few minutes.

At this point the next boot should start the Dashcam/music drives like normal. If you're watching the LED it will start flashing every 1 second, which is the archive loop running.

> **Note** Don't delete the `SENTRYUSB_SETUP_FINISHED` or `WIFI_ENABLED` files. This is how the system knows setup is complete.

# Image modification sources

The sources for the image modifications, and instructions, are in the [pi-gen-sources folder](https://github.com/Scottmg1/Sentry-USB/tree/main-dev/pi-gen-sources).
