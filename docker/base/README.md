To speed up the building of each image, banbot recommends using phased building.
为加快每次镜像的构建速度，banbot推荐使用分阶段构建。

## 1. Build the banbase (构建基础镜像banbase)
The base image only needs to be built once, and you can directly execute step 2 each time afterwards.

基础镜像只需构建一次，后续每次直接执行步骤2即可。
```shell
docker build -f base.Dockerfile -t banbase .
```

## 2. Build the latest image (构建最新镜像)
```shell
docker build --no-cache -t banbot/banbot .
```
