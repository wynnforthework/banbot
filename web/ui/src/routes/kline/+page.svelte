<script lang="ts">
  import Chart from "$lib/kline/Chart.svelte";
  import { writable } from "svelte/store";
  import { persisted } from "svelte-persisted-store";
  import { ChartCtx, ChartSave } from "$lib/kline/chart";
  import {site} from '$lib/stores/site';
  import {dev} from '$app/environment';
  import {derived} from 'svelte/store';
  
  site.update((s) => {
    if(dev){
      s.apiHost = 'http://localhost:8000';
    }
    s.apiReady = true;
    return s;
  });

  const kcCtx = writable<ChartCtx>(new ChartCtx());
  let saveRaw = new ChartSave();
  saveRaw.key = 'chart';
  const kcSave = persisted(saveRaw.key, saveRaw);
  let kc: Chart;

  
  const klineLoad = derived(kcCtx, ($ctx) => $ctx.initDone);
  klineLoad.subscribe(val => {
    $kcCtx.fireOhlcv += 1;
  })
</script>

<Chart bind:this={kc} ctx={kcCtx} save={kcSave} customLoad={true} />
