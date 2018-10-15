% podman-tree "1"

## NAME
podman\-tree - Show dependent layers of a specified image in tree format

## SYNOPSIS
**podman tree** [*image*:*tag*]**|**[*image-id*]
[**--help**|**-h**]

## DESCRIPTION
Displays the dependent layers of an image. The information is printed in tree format.
If you do not provide *tag*, podman will default to `latest` for the *image*.

## OPTIONS

**--help**, **-h**

Print usage statement

## EXAMPLES

```
$ podman tree fedora:latest

$ podman tree 243d869d10ea
```


## SEE ALSO
podman(1), crio(8)

## HISTORY
Oct 2018, Originally compiled by Kunal Kushwaha <kushwaha_kunal_v7@lab.ntt.co.jp>
