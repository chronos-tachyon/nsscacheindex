# nsscacheindex
Generates binary search index files for libnss-cache

Usage:

```
$ nsscacheindex -s /etc/passwd.cache  -d /etc/passwd.cache.ixname  -c 1
$ nsscacheindex -s /etc/passwd.cache  -d /etc/passwd.cache.ixuid   -c 3 -n
$ nsscacheindex -s /etc/group.cache   -d /etc/group.cache.ixname   -c 1
$ nsscacheindex -s /etc/group.cache   -d /etc/group.cache.ixgid    -c 3 -n
$ nsscacheindex -s /etc/shadow.cache  -d /etc/shadow.cache.ixname  -c 1
$ nsscacheindex -s /etc/gshadow.cache -d /etc/gshadow.cache.ixname -c 1
```
