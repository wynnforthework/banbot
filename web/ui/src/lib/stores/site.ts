import { writable } from "svelte/store";

class SiteState {
  building: boolean = false
  dirtyBin: boolean = false // 标志是否需要重新构建
  apiHost: string = ''
  apiReady: boolean = true
  apiReadyCbs: (() => void)[] = []
}

export const site = writable(new SiteState())
