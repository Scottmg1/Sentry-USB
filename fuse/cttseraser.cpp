/*
 * MIT License
 *
 * Copyright (c) 2021 Marco Nelissen
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

/*
  Read-only fuse filesystem that erases the 'ctts' tag in mp4
  files to work around Chromium-based browsers not playing
  Tesla recordings.

  Build:
  g++ -o cttseraser -D_FILE_OFFSET_BITS=64 cttseraser.cpp -lstdc++ -lfuse

  To run manually:
  cttseraser <sourcedir> -f <mountpoint>
  e.g.
  cttseraser /mutable/TeslaCam -f /var/www/html/TeslaCam
*/

#define FUSE_USE_VERSION 30
#include <fuse.h>

#include <arpa/inet.h>
#include <errno.h>
#include <dirent.h>
#include <linux/limits.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/sysmacros.h>
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
#include <time.h>
#include <unistd.h>
#include <new>

const char *source;

static void convert_timestamp(struct timespec* ts, struct statx_timestamp* xts) {
  if (sizeof(ts->tv_sec) == 4 && xts->tv_sec > INT32_MAX) {
    ts->tv_sec = INT32_MAX;
  } else {
    ts->tv_sec = xts->tv_sec;
  }
  ts->tv_nsec = xts->tv_nsec;
}

static int statx2stat(struct stat* st, struct statx* stx) {
  memset(st, 0, sizeof(*st));

  if (stx->stx_mask & STATX_TYPE) {
    st->st_mode = stx->stx_mode & S_IFMT;
  }

  if (stx->stx_mask & STATX_MODE) {
    st->st_mode |= stx->stx_mode & ~S_IFMT;
  }

  if (stx->stx_mask & STATX_NLINK) {
    st->st_nlink = stx->stx_nlink;
  }

  if (stx->stx_mask & STATX_UID) {
    st->st_uid = stx->stx_uid;
  }

  if (stx->stx_mask & STATX_GID) {
    st->st_gid = stx->stx_gid;
  }

  if (stx->stx_mask & STATX_INO) {
    st->st_ino = stx->stx_ino;
  }

  if (stx->stx_mask & STATX_SIZE) {
    st->st_size = stx->stx_size;
  }

  if (stx->stx_mask & STATX_BLOCKS) {
    st->st_blocks = stx->stx_blocks;
  }

  if (stx->stx_mask & STATX_ATIME) {
    convert_timestamp(&st->st_atim, &stx->stx_atime);
  }

  if (stx->stx_mask & STATX_MTIME) {
    convert_timestamp(&st->st_mtim, &stx->stx_mtime);
  }

  if (stx->stx_mask & STATX_CTIME) {
    convert_timestamp(&st->st_ctim, &stx->stx_ctime);
  }

  st->st_rdev = makedev(stx->stx_rdev_major, stx->stx_rdev_minor);
  st->st_dev = makedev(stx->stx_dev_major, stx->stx_dev_minor);

  return 0;
}

static int mystat(const char* path, struct stat *st) {
  int ret = 0;
#if 0
  ret = stat(path, st);
  if (ret == 0) {
    return 0;
  }
  printf("do_getattr: stat(%s) returned %d (%d: %s)\n", path, ret, errno, strerrorname_np(errno));
#endif

  struct statx stx;
  ret = statx(0, path, 0, STATX_BASIC_STATS, &stx);

  if (ret == 0) {
    return statx2stat(st, &stx);
  }
  printf("do_getattr: statx(%s) returned %d (%d: %s)\n", path, ret, errno, strerror(errno));

  ret = lstat(path, st);
  if (ret == 0) {
    return 0;
  }
  printf("do_getattr: lstat(%s) returned %d (%d: %s)\n", path, ret, errno, strerror(errno));
  return -errno;
}

static int do_getattr( const char *path, struct stat *st ) {
  printf("do_getattr(%s)\n", path);
  char pathbuf[PATH_MAX];
  snprintf(pathbuf, sizeof(pathbuf), "%s%s", source, path);
  return mystat(pathbuf, st);
}

static int do_readdir(const char *path, void *buffer, fuse_fill_dir_t filler,
                      off_t offset, struct fuse_file_info *fi ) {
  printf("do_readdir(%s)\n", path);

  char pathbuf[PATH_MAX];
  snprintf(pathbuf, sizeof(pathbuf), "%s%s", source, path);
  DIR *dir = opendir(pathbuf);
  if (!dir) {
    return -1;
  }

  while (true) {
    struct dirent *ent = readdir(dir);
    if (!ent) {
      break;
    }
    filler(buffer, ent->d_name, NULL, 0);
  }
  closedir(dir);
  return 0;
}

template <size_t N>
constexpr uint32_t FOURCC(const char (&s) [N]) {
    static_assert(N == 5, "fourcc: wrong length");
    return
            (unsigned char) s[0] << 24 |
            (unsigned char) s[1] << 16 |
            (unsigned char) s[2] << 8 |
            (unsigned char) s[3] << 0;
}

typedef struct {
  int32_t size;
  uint32_t fourcc;
} chunkinfo;

static chunkinfo get_chunk(int fd, int offset) {
  chunkinfo info;
  if (lseek(fd, offset, SEEK_SET) < 0) {
    info.size = -1;
    return info;
  }
  uint32_t val;
  if (read(fd, &val, sizeof(val)) != sizeof(val)) {
    info.size = -1;
    return info;
  }
  info.size = ntohl(val);

  if (read(fd, &val, sizeof(val)) != sizeof(val)) {
    info.size = -1;
    return info;
  }
  info.fourcc = ntohl(val);
  return info;
}

static int parse_chunks(int fd, int start, int end) {
  while (true) {
    if (start > (end - 8)) {
      return 0;
    }
    chunkinfo info = get_chunk(fd, start);
    if (info.size < 0) {
      return -1;
    }

    switch (info.fourcc) {
      case FOURCC("ctts"):
        return start;

      case FOURCC("moov"):
      case FOURCC("trak"):
      case FOURCC("mdia"):
      case FOURCC("minf"):
      case FOURCC("stbl"):
        {
          int off = parse_chunks(fd, start + 8, start + info.size);
          if (off < 0) {
            return -1;
          }
          if (off > 0) {
            return off;
          }
        }
        break;
    }
    start += info.size;
  }
  return 0;
}

static int find_ctts(int fd) {
  chunkinfo info = get_chunk(fd, 0);
  if (info.fourcc != FOURCC("ftyp")) {
    return 0;
  }

  return parse_chunks(fd, info.size, 99999999);
}

typedef struct {
  int fd;
  int cttsoffset;
} filehandle;

static int do_open(const char *path, struct fuse_file_info *fi) {
  printf("do_open(%s)\n", path);

  if ((fi->flags & (O_WRONLY || O_RDWR)) != 0) {
    printf("write not allowed\n");
    return -1;
  }
  char pathbuf[PATH_MAX];
  snprintf(pathbuf, sizeof(pathbuf), "%s%s", source, path);

  int fd = open(pathbuf, O_RDONLY);
  if (fd < 0) {
    printf("couldn't open %s\n", pathbuf);
    return -1;
  }
  int cttsoffset = find_ctts(fd);
  printf("cttsoffset: %d\n", cttsoffset);
  filehandle *fh = new (std::nothrow) filehandle;
  if (!fh) {
    close(fd);
    return -1;
  }
  fh->fd = fd;
  fh->cttsoffset = cttsoffset;
  fi->fh = (uint64_t) fh;
  return 0;
}

int do_release(const char *, struct fuse_file_info *fi) {
  filehandle *fh = (filehandle*) fi->fh;
  int fd = fh->fd;
  printf("do_release(%d)\n", fd);
  close(fd);
  delete fh;
  return 0;
}

#define max(a,b) ((a) > (b) ? (a) : (b))
#define min(a,b) ((a) < (b) ? (a) : (b))

static int do_read( const char *path, char *buffer, size_t size, off_t offset, struct fuse_file_info *fi ) {
  printf("do_read(%s, %lld, %zu)\n", path, (long long) offset, size);

  char pathbuf[PATH_MAX];
  snprintf(pathbuf, sizeof(pathbuf), "%s%s", source, path);

  filehandle *fh = (filehandle*) fi->fh;
  int fd = fh->fd;

  int numread = pread(fd, buffer, size, offset);
  if (numread < 0) {
    printf("couldn't read\n");
    return -1;
  }

  /*
         offset |--------------------| offset + size
     ctts:   |--|
     ctts:    |--|
     ctts:              |--|
     ctts:                         |--|
     ctts:                           |--|
  */

  int ctts = fh->cttsoffset;
  if (ctts > 0) {
    ctts += 4; // location of fourcc
    int s = max(offset, ctts);
    int e = min(offset + size, ctts + 4);
    if (e > s) {
      /* buffer includes (part of) ctts fourcc */
      memset(buffer + (s - offset), '@', e - s);
      printf("clearing ctts at offset %d, len %d\n", int(s - offset), int(e-s));
    }
  }

  return numread;
}

static struct fuse_operations ops = {
    .getattr = do_getattr,
    .open    = do_open,
    .read    = do_read,
    .release = do_release,
    .readdir = do_readdir,
};

int main( int argc, char *argv[] ) {
  if (argc < 3) {
    return 1;
  }
  source = argv[1];
  struct stat statbuf;
  if (stat(source, &statbuf) < 0) {
    printf("%s does not exist\n", source);
    return 1;
  }
  argc--;
  for (int i = 1; i < argc; i++) {
    argv[i] = argv[i + 1];
  }

  return fuse_main( argc, argv, &ops, NULL );
}
