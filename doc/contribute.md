
## Package Dependencies
```text
core
  --
btime
  core
utils
  core btime
config
  core btime utils
exg
  config utils core
orm
  exg config  
data
  orm exg config 
strategy
  orm utils
goods
  strategy orm exg 
biz:
  exg orm strategy goods data
optmize
  biz data orm goods strategy
live 
  biz data orm goods strategy
entry
  optmize live data 
```
