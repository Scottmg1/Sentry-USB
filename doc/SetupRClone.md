# Introduction

This guide will show you how to install and configure [rclone](https://rclone.org/) to archive your saved TeslaCam footage on one of a number of different remote storage services including Google Drive, S3 and Dropbox.

# Easy rclone setup

The easiest way to to configure SentryUSB for rclone is:

- use the [one-step setup process](OneStepSetup.md) first, but select the `none` archive method instead of `cifs`. Once setup has completed, you will have a Raspberry Pi based USB drive that works with the car, but that doesn't do any archiving. Make sure the Pi is fully functional before proceeding with the next steps.
- ssh into the Pi, become root and remount the filesystems read-write:
  ```
  sudo -i
  /root/bin/remountfs_rw
  ```
- install rclone: `curl https://rclone.org/install.sh | sudo bash`
- configure rclone for your chosen storage service: `rclone config`, then follow the instructions from [rclone.org](https://rclone.org/)
- edit "/root/sentryusb.conf" and change the archive method to `rclone`
- add the RCLONE_DRIVE and RCLONE_PATH variables to the config, according to the values you used when you configured the rclone remote.
  RCLONE_DRIVE should be a name shown by `rclone listremotes`, and RCLONE_PATH should be a path that exists on the named remote, i.e. `rclone ls "$RCLONE_DRIVE:$RCLONE_PATH"` should not print an error (but it may print nothing, if the path is newly created and currently empty).
- optionally populate RCLONE_FLAGS with flags to add to the command, i.e. RCLONE_FLAGS=(--checksum). To specify multiple flags: RCLONE_FLAGS=(--flag1 --flag2)
  ```
  export RCLONE_DRIVE="remotename"
  export RCLONE_PATH="remotepathname"
  export RCLONE_FLAGS=()
  ```
- run `/root/bin/setup-sentryusb`

Below are the old instructions in case you want to do things the hard way.

# Legacy instructions

> **Note** it is recommended you follow the "Easy rclone setup" instructions listed above instead

You must perform these steps **after** getting a shell on the Pi and **before** running the `setup-teslacam` script on the Pi.

**Make sure to run all commands in these instructions in a single command shell as root. When you return to the [Main Instructions](/README.md) continue running the commands there in this same shell.** This is necessary because:

- The `archiveloop` script runs as root and the rclone config is bound to the user running the config.
- These commands define environment variables that the main setup scripts need.

# Quick guide

These instructions will speed you through the process with good defaults. If you encounter an error, or you want to use a different config name than `gdrive` or a different folder name than `TeslaCam`, follow the detailed instuctions, below.

1. Enter the root session if you haven't already:
   ```
   sudo -i
   ```
2. Run these commands. Specify the config name `gdrive` when prompted for the config name.
   ```
   curl https://rclone.org/install.sh | sudo bash
   rclone config
   ```
3. Run these commands:

   ```
   export ARCHIVE_SYSTEM=rclone
   export RCLONE_DRIVE=gdrive
   export RCLONE_PATH=TeslaCam

   rclone mkdir "$RCLONE_DRIVE:$RCLONE_PATH"
   rclone lsd "$RCLONE_DRIVE":
   ```

4. If you didn't encounter any error messages and you see the `TeslaCam` directory listed, stay in your `sudo -i` session and return to the [Main Instructions](../README.md).

# Detailed instructions

## Step 1: Install rclone

1. Enter a root session on your Pi (if you haven't already):
   ```
   sudo -i
   ```
2. Run the following command to install rclone:
   ```
   curl https://rclone.org/install.sh | sudo bash
   ```
   Alternatively, you can install rclone manually by following these [instructions](https://rclone.org/install/).

# Step 2: Configure the archive

1. Run this command to configure an archive:

   ```
   rclone config
   ```

   This will launch an interactive setup with a series of questions. It is recommended that you look at the documentation for your storage system by going to [rclone](https://rclone.org/) and selecting your storage system from the pull down menu at the stop.

   It has been confirmed that this process works with Google Drive using these [instructions](https://rclone.org/drive/). If you are using another storage system, please feel encouraged to create an "Issue" describing your challenges and/or your success.

   If you are using Google Drive it is important to set the correct [scope](https://rclone.org/drive/#scopes). Carefully read the documentation on [scopes on rclone](https://rclone.org/drive/#scopes) as well as [Google Drive](https://developers.google.com/drive/api/v3/about-auth). The `drive.file` scope is recommended.

   **Important:** During the `rclone config` process you will specify a name for the configuration. The rest of the document will assume the use of the name `gdrive`; replace this with your chosen configuration name.

2. Run this command:
   ```
   export RCLONE_DRIVE="gdrive"
   ```

# Step 3: Verify and create storage directory

1. Run the following command to see the name of the remote drive you just created.
   ```
   rclone listremotes
   ```
   If you don't see the name there, something went wrong. Go back through the `rclone config` process.
2. Run this command:
   ```
   rclone lsd "$RCLONE_DRIVE":
   ```
   You should not see any files listed. If you do then you did not set your scope correctly during the `rclone config` process.
3. Choose the name of a folder to hold the archived clips. These instructions will assume you chose the name `TeslaCam`. Substitute the name you chose for this name. Run this command:
   ```
   export RCLONE_PATH="TeslaCam"
   ```
4. Run the following command to create a folder which will hold the archived clips.
   ```
   rclone mkdir "$RCLONE_DRIVE:TeslaCam"
   ```
5. Run this command again:
   ```
   rclone lsd "$RCLONE_DRIVE":
   ```
   Confirm that the directory `TeslaCam` is present. If not, start over.

# Step 4: Exports

Run this command to cause the setup processes which you'll resume in the main instructions to use rclone:

```
export ARCHIVE_SYSTEM=rclone
```

Now stay in your `sudo -i` session and return to the section "Set up the USB storage functionality" in the [main instructions](../README.md).
