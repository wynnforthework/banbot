import { writable } from "svelte/store";

class SiteConfig {
  apiHost: string = ''
  apiReady: boolean = true
  apiReadyCbs: (() => void)[] = []
}

export const site = writable({
  apiHost: '',
  apiReady: false,
  apiReadyCbs: [] as (() => void)[]
})
