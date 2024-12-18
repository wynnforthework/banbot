<script lang="ts">
  import * as m from '$lib/paraglide/messages.js'
  import Icon from '@/lib/Icon.svelte';
  let { children } = $props();
  import {site} from '@/lib/config';
  import {dev} from '$app/environment';
  site.update((s) => {
    if(dev){
      s.apiHost = 'http://localhost:8000';
    }
    s.apiReady = true;
    return s;
  });
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
    
    <div class="navbar-end"></div>
  </div>

  <main class="flex-1 flex flex-col">
    {@render children()}
  </main>
</div>
