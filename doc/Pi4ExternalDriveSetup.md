# Setting up external USB drive (SSD) with Pi4

This guide will walk you through how to use Pi4 and a separate USB drive to to host your CAM and MUSIC files.

#### Limitations

- Early release. Still being tested - 9.2019
- Supports manual setup. Not tested yet with headless setup.
- Pi4 does not support booting from USB drive. A SD card is still needed for boot. It can be as small as 4GB and no high endurance required as most data will be written and read from the attached USB.
- Will erase entire disk. All data on disk will be lost.
- Cannot resize existing partitions.

## Hardware

1. Raspberry Pi4 - Any RAM option will work.
2. At least 4GB SD card - !! ONLY IF USING AN EXTERNAL DRIVE !! Refer to main page if you are not using external drive.
3. USB Drive - SSD preferred due to low power requirements.
4. Optional, but highly recommended - Heatsink case as Pi4 can get very hot.
5. Optional - X855 mSata board for a more compact setup.

## sentryusb.conf configuration

To use an external USB drive, you will need to add
`export USB_DRIVE=/dev/sdX` to sentryusb.conf file.
Ensure that you are providing the disk location and not a partition.

The rest of the setup is the same as the installation steps found in the main page. Both /backingfiles and /mutable will be on the external drive and the SD card will be read-only.
