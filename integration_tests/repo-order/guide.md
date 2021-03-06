# repo-order
tags: repo-order

## Description
Environment to demonstrate that packages will be sourced from repositories in the order
that the repositories are listed. In other words, if your YAML file looks like
this...

```
Packages:
  - packageA

# any repositories, order matters
Repos:
  - Repo1: "www.repo1.io"
  - Repo2: "www.repo2.io"
```
...then pkgr will search Repo1 for packageA first. If Repo1 contains packageA, then
pkgr will use the latest version of packageA from Repo1. Otherwise, it will check
Repo2.

## Expected Behavior:

* `pkgr plan --loglevel=debug` indicates that mrgsolve is being pulled
from CRAN and not r_validated:
```
DEBU[0000] package repository set                        pkg=mrgsolve relationship="user package" repo=CRAN type=source version=0.10.1
```
