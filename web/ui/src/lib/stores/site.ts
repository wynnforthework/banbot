import { writable } from "svelte/store";

class SiteState {
  building: boolean = false
  dirtyBin: boolean = false // 标志是否需要重新构建
  compileNeed: boolean = false
  apiHost: string = ''
  apiReady: boolean = true
  apiReadyCbs: (() => void)[] = []
  heavyName: string = ''
  heavyDone: number = 0
  heavyTotal: number = 0
}

export const site = writable(new SiteState())
