#!/bin/bash -eu

if [ "${BASH_SOURCE[0]}" != "$0" ]
then
  echo "${BASH_SOURCE[0]} must be executed, not sourced"
  return 1 # shouldn't use exit when sourced
fi

if [ "${FLOCKED:-}" != "$0" ]
then
  mkdir -p /backingfiles/snapshots
  if FLOCKED="$0" flock -E 99 /backingfiles/snapshots "$0" "$@" || case "$?" in
  99) echo "failed to lock snapshots dir"
      exit 99
      ;;
  *)  exit $?
      ;;
  esac
  then
    # success
    exit 0
  fi
fi

function linksnapshotfiletorecents {
  local file=$1
  local curmnt=$2
  local finalmnt=$3
  local recents=/mutable/TeslaCam/RecentClips

  filename=${file##/*/}
  if [[ ! "$filename" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}.* ]]
  then
    return
  fi

  filedate=${filename:0:10}
  if [ ! -d "$recents/$filedate" ]
  then
    mkdir -p "$recents/$filedate"
  fi
  ln -sf "${file/"$curmnt"/$finalmnt}" "$recents/$filedate"
}

function make_links_for_snapshot {
  local saved=/mutable/TeslaCam/SavedClips
  local sentry=/mutable/TeslaCam/SentryClips
  local track=/mutable/TeslaCam/TeslaTrackMode
  if [ ! -d $saved ]
  then
    mkdir -p $saved
  fi
  if [ ! -d $sentry ]
  then
    mkdir -p $sentry
  fi
  local curmnt="$1"
  local finalmnt="$2"
  log "making links for $curmnt, retargeted to $finalmnt"
  local restore_nullglob
  restore_nullglob=$(shopt -p nullglob)
  shopt -s nullglob
  for f in "$curmnt/TeslaCam/RecentClips/"*
  do
    #log "linking $f"
    linksnapshotfiletorecents "$f" "$curmnt" "$finalmnt"
  done
  # also link in any files that were moved to SavedClips
  for f in "$curmnt/TeslaCam/SavedClips"/*/*
  do
    #log "linking $f"
    linksnapshotfiletorecents "$f" "$curmnt" "$finalmnt"
    # also link it into a SavedClips folder
    local eventfolder=${f%/*}
    local eventtime=${eventfolder##/*/}
    if [ ! -d "$saved/$eventtime" ]
    then
      mkdir -p "$saved/$eventtime"
    fi
    ln -sf "${f/$curmnt/$finalmnt}" "$saved/$eventtime"
  done
  # and the same for SentryClips
  for f in "$curmnt/TeslaCam/SentryClips/"*/*
  do
    #log "linking $f"
    linksnapshotfiletorecents "$f" "$curmnt" "$finalmnt"
    local eventfolder=${f%/*}
    local eventtime=${eventfolder##/*/}
    if [ ! -d "$sentry/$eventtime" ]
    then
      mkdir -p "$sentry/$eventtime"
    fi
    ln -sf "${f/$curmnt/$finalmnt}" "$sentry/$eventtime"
  done
  # and finally the TrackMode files
  for f in "$curmnt/TeslaTrackMode/"*
  do
    if [ ! -d "$track" ]
    then
      mkdir -p "$track"
    fi
    ln -sf "$f" "$track"
  done
  log "made all links for $curmnt"
  $restore_nullglob
}

function snapshot {
  # since taking a snapshot doesn't take much extra space, do that first,
  # before cleaning up old snapshots to maintain free space.
  local oldnum=-1
  local newnum=0
  if stat /backingfiles/snapshots/snap-*/snap.bin > /dev/null 2>&1
  then
    oldnum=$(find /backingfiles/snapshots/snap-* -maxdepth 1 -name snap.bin | sort | tail -1 | tr -c -d '[:digit:]' | sed 's/^0*//' )
    newnum=$((oldnum + 1))
  fi
  local oldname
  local newsnapdir
  oldname=/backingfiles/snapshots/snap-$(printf "%06d" "$oldnum")/snap.bin

  # check that the previous snapshot is complete
  if [ ! -e "${oldname}.toc" ] && [ "$oldnum" != "-1" ]
  then
    log "previous snapshot was incomplete, deleting"
    rm -rf "$(dirname "$oldname")"
    newnum=$((oldnum))
    oldnum=$((oldnum - 1))
    oldname=/backingfiles/snapshots/snap-$(printf "%06d" "$oldnum")/snap.bin
  fi

  newsnapdir=/backingfiles/snapshots/snap-$(printf "%06d" $newnum)
  newsnapmnt=/tmp/snapshots/snap-$(printf "%06d" $newnum)

  local newsnapname=$newsnapdir/snap.bin
  log "taking snapshot of cam disk in $newsnapdir"

  if mount | grep /backingfiles/cam_disk.bin
  then
    echo "snapshot already mounted"
  fi

  SNAPDIR=$(dirname "$newsnapname")
  if [ ! -d "$SNAPDIR" ]
  then
    mkdir -p "$SNAPDIR"
  fi

  if [ -e "$newsnapname" ]
  then
    umount "$newsnapmnt" || true
    rm -rf "$newsnapname"
  fi

  # make a copy-on-write snapshot of the current image
  cp --reflink=always /backingfiles/cam_disk.bin "$newsnapname"
  # at this point we have a snapshot of the cam image, which is completely
  # independent of the still in-use image exposed to the car

  # create loopback and scan the partition table, this will create an additional
  # loop device in addition to the main loop device, e.g. /dev/loop0 and
  # /dev/loop0p1

  # Use -p repair arg. It works with vfat and exfat.
  LOOP=$(losetup_find_show -P "$newsnapname")
  PARTLOOP=${LOOP}p1

  if [ "$1" = "fsck" ]
  then
    fsck "$PARTLOOP" -- -p || true
  fi

  losetup -d "$LOOP"

  # if needed, manually mount the image and check/fix timestamps
  if [ "$(getconf LONG_BIT)" = "32" ] && [ "$(. /etc/os-release && echo "${VERSION_ID:-}")" = "12" ]
  then
    local -r tmpmnt=$(mktemp -d)
    /root/bin/mountimage "$newsnapname" "$tmpmnt" rw
    find "$tmpmnt" -newerat 20380101 | xargs -r touch
    umount "$tmpmnt"
    rmdir "$tmpmnt"
  fi

  while ! systemctl --quiet is-active autofs
  do
    log "waiting for autofs to be active"
    sleep 1
  done
  log "took snapshot"

  # check whether this snapshot is actually different from the previous one
  find "$newsnapmnt" -type f -printf '%s %P\n' > "${newsnapname}.toc_"
  log "comparing new snapshot with $oldname"
  if [[ ! -e "${oldname}.toc" ]] || diff "${oldname}.toc" "${newsnapname}.toc_" | grep -qe '^>'
  then
    ln -s "$newsnapmnt" "$newsnapdir/mnt"
    make_links_for_snapshot "$newsnapmnt" "$newsnapdir/mnt"
    mv "${newsnapname}.toc_" "${newsnapname}.toc"
  else
    log "new snapshot is identical to previous one, discarding"
    /root/bin/release_snapshot.sh "$newsnapdir"
    rm -rf "$newsnapdir"
  fi
}

if ! snapshot "${1:-fsck}"
then
  log "failed to take snapshot"
fi
