<script lang="ts">
  import * as m from '$lib/paraglide/messages.js'
  import Icon from '@/lib/Icon.svelte';
  let { children } = $props();
  import {site} from '@/lib/stores/site';
  import {dev} from '$app/environment';
  import {initWsConn} from '@/lib/dev/websocket';
  import {browser} from '$app/environment';
  import {alerts} from '@/lib/stores/alerts';
  import { postApi, getApi } from '@/lib/netio';
  
  site.update((s) => {
    if(dev){
      s.apiHost = 'http://localhost:8000';
    }
    s.apiReady = true;
    return s;
  });
  
  if(browser){
    initWsConn();
  }
  
  let showLogs = $state(false);
  let logs = $state('');
  let lastStart: number | null = null;
  
  async function fetchLogs(end?: number) {
    const params = end ? { end } : {};
    const res = await getApi('/dev/logs', params);
    if (res.code === 200) {
      if (end) {
        logs = logs + res.data;
      } else {
        logs = res.data;
      }
      lastStart = res.start;
    } else {
      console.error('get logs failed', res);
      alerts.addAlert('error', res.msg || 'get logs failed');
    }
  }

  function toggleLogs() {
    showLogs = !showLogs;
    if (showLogs && !logs) {
      fetchLogs();
    }
  }
  
  async function clickRefresh() {
    if ($site.building) {
      alerts.addAlert('warning', m.already_building());
      return;
    }
    const res = await postApi('/dev/build', {});
    if (res.code !== 200) {
      console.error('build failed', res);
      alerts.addAlert('error', res.msg || 'build failed');
    }
  }
</script>

<div class="flex flex-col min-h-screen">
  <div class="navbar bg-base-100 shadow-md py-0">
    <div class="navbar-start">
      <a href="/" class="btn btn-ghost text-xl">Banbot</a>
    </div>
    
    <div class="navbar-center">
      <!-- 移动端的下拉菜单 -->
      <div class="dropdown lg:hidden">
        <div tabindex="0" role="button" class="btn btn-ghost">
          <Icon name="horz3"/>
        </div>
        <ul tabindex="0" class="menu menu-sm dropdown-content bg-base-100 rounded-box z-[1] mt-3 w-52 p-2 shadow">
          <li><a href="/strategy" class="py-3"><Icon name="code"/>{m.strategy()}</a></li>
          <li><a href="/backtest" class="py-3"><Icon name="calculate"/>{m.backtest()}</a></li>
          <li><a href="/optimize" class="py-3"><Icon name="cpu"/>{m.optimize()}</a></li>
          <li><a href="/data" class="py-3"><Icon name="chart-bar"/>{m.data()}</a></li>
          <li><a href="/tools" class="py-3"><Icon name="tool"/>{m.tools()}</a></li>
        </ul>
      </div>
      
      <!-- 桌面端的水平菜单 -->
      <ul class="menu menu-horizontal px-1 hidden lg:flex">
        <li><a href="/strategy"><Icon name="code"/>{m.strategy()}</a></li>
        <li><a href="/backtest"><Icon name="calculate"/>{m.backtest()}</a></li>
        <li><a href="/optimize"><Icon name="cpu"/>{m.optimize()}</a></li>
        <li><a href="/data"><Icon name="chart-bar"/>{m.data()}</a></li>
        <li><a href="/tools"><Icon name="tool"/>{m.tools()}</a></li>
      </ul>
    </div>
    
    <div class="navbar-end">
      <div class="tooltip tooltip-bottom" data-tip={m.build()}>
        <button class="btn btn-ghost btn-circle" 
                class:animate-spin={$site.building}
                onclick={clickRefresh}>
          <Icon name="refresh"/>
        </button>
      </div>
      <div class="tooltip tooltip-bottom" data-tip={m.logs()}>
        <button class="btn btn-ghost btn-circle" onclick={toggleLogs}>
          <Icon name="document-text"/>
        </button>
      </div>
    </div>
  </div>

  <!-- 添加日志抽屉 -->
  <div class="drawer drawer-end flex-1 flex flex-col">
    <input id="logs-drawer" type="checkbox" class="drawer-toggle" bind:checked={showLogs} />
    
    <div class="drawer-content flex-1 flex flex-col">
      <!-- 主页面内容 -->
      <main class="flex-1 flex flex-col">
        {@render children()}
      </main>
    </div>
    
    <div class="drawer-side">
      <label for="logs-drawer" aria-label="close logs" class="drawer-overlay"></label>
      <div class="bg-base-200 w-2/3 h-full p-4 flex flex-col">
        <!-- 日志抽屉顶部按钮 -->
        <div class="flex justify-end gap-2 mb-4">
          <button class="btn btn-ghost btn-sm" 
                  onclick={() => lastStart && fetchLogs(lastStart)}>
            <Icon name="arrow-down"/>{m.load_more()}
          </button>
          <button class="btn btn-ghost btn-sm" 
                  onclick={() => fetchLogs()}>
            <Icon name="refresh"/>{m.refresh()}
          </button>
        </div>
        
        <!-- 日志内容 -->
        <pre class="flex-1 overflow-auto whitespace-pre-wrap">{logs}</pre>
      </div>
    </div>
  </div>
</div>
