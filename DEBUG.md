### Debug remote Docker

```shell
ssh -nNT -L /tmp/docker.sock:/var/run/docker.sock root@remote
```

```shell
export DOCKER_HOST="unix:///tmp/docker.sock"
```
