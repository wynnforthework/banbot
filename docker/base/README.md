To speed up the building of each image, banbot recommends using phased building.
为加快每次镜像的构建速度，banbot推荐使用分阶段构建。

## 1. Build the banbase (构建基础镜像banbase)
The base image only needs to be built once, and you can directly execute step 2 each time afterwards.

基础镜像只需构建一次，后续每次直接执行步骤2即可。（推荐在访问docker仓库流畅的机器上运行）
```shell
docker build -f base.Dockerfile --no-cache -t banbot/banbase .
docker push banbot/banbase
```
After a large number of dependency packages have been upgraded, the base image should be rebuilt and pushed again to avoid downloading a large number of dependency packages in Step 2 each time.

当进行了大量依赖包升级后，应当重新执行基础镜像构建并推送，避免步骤2每次下载大量依赖包。

## 2. Build the latest image (构建最新镜像)
```shell
docker build --no-cache -t banbot/banbot .
```
This step has created a GitHub Workflow in [banstrats](https://github.com/banbox/banstrats), which automatically builds and pushes to the Docker Hub repository whenever the code is updated.

此步骤已在[banstrats](https://github.com/banbox/banstrats)中创建github workflow，每次更新代码会自动构建并推送到docker hub仓库。
