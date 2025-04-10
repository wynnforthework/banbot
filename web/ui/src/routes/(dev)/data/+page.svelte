<script lang="ts">
  import { onMount } from 'svelte';
  import {getApi} from '$lib/netio'
  import type { ExSymbol } from '$lib/dev/common';
  import * as m from '$lib/paraglide/messages.js';
  import DrawerDataTools from '$lib/dev/DrawerDataTools.svelte';
  import { exchanges, markets } from '$lib/common';
  import {curTZ, fmtDateStr} from '$lib/dateutil';
  import { pagination } from '$lib/Snippets.svelte';
  import {localizeHref} from "$lib/paraglide/runtime";

  // 状态变量
  let symbols: ExSymbol[] = $state([]);
  let totalCount = $state(0);
  let currentPage = $state(1);
  let pageSize = $state(20);

  // 工具抽屉状态
  let isDrawerOpen = $state(false);

  // 筛选条件
  let selectedExchange = $state('');
  let selectedMarket = $state('');
  let symbolFilter = $state('');
  let lastId = $state(0);

  // 获取数据
  async function fetchSymbols() {
    const rsp = await getApi(`/dev/symbols`, {
      exchange: selectedExchange,
      market: selectedMarket,
      symbol: symbolFilter,
      limit: pageSize,
      after_id: lastId,
    });
    symbols = rsp.data;
    totalCount = rsp.total;
  }

  // 应用筛选
  function applyFilters() {
      currentPage = 1;
      lastId = 0;
      console.log('applyFilters');
      fetchSymbols();
  }

  // 监听页面变化
  $effect(() => {
    if (currentPage > 1) {
      setTimeout(() => {
        lastId = symbols[symbols.length - 1]?.id || 0;
        fetchSymbols();
      }, 10);
    }
  });

  function toggleDrawer() {
    isDrawerOpen = !isDrawerOpen;
  }
  onMount(() => {
    fetchSymbols()
  });
</script>

<div class="container mx-auto p-4">
    <div class="flex justify-between items-center mb-4">
        <div class="flex gap-4">
            <select
                class="select w-full"
                bind:value={selectedExchange}
                onchange={applyFilters}
            >
                <option value="">{m.all_exchanges()}</option>
                {#each exchanges as exchange}
                    <option value={exchange}>{exchange}</option>
                {/each}
            </select>

            <select
                class="select w-full"
                bind:value={selectedMarket}
                onchange={applyFilters}
            >
                <option value="">{m.all_markets()}</option>
                {#each markets as market}
                    <option value={market}>{market}</option>
                {/each}
            </select>

            <input
                type="text"
                placeholder={m.search_symbol()}
                class="input w-full max-w-lg"
                bind:value={symbolFilter}
                oninput={applyFilters}
            />
        </div>

        <div class="flex items-center gap-2">
            <a href={localizeHref("/kline")} target="_blank" class="btn btn-primary btn-outline m-1">{m.kline()}</a>
            <button class="btn btn-primary m-1" onclick={toggleDrawer}>{m.tools()}</button>
        </div>
    </div>

    <div class="overflow-x-auto w-full" style="min-height: 24rem;">
        <table class="table table-zebra w-full">
            <thead>
                <tr>
                    <th>ID</th>
                    <th>{m.exchange()}</th>
                    <th>{m.exg_real()}</th>
                    <th>{m.market()}</th>
                    <th>{m.symbol()}</th>
                    <th>{m.combined()}</th>
                    <th>{m.list_time()}({curTZ()})</th>
                    <th>{m.delist_time()}({curTZ()})</th>
                    <th>-</th>
                </tr>
            </thead>
            <tbody>
                {#each symbols as symbol}
                    <tr class="hover:bg-base-300">
                        <td>{symbol.id}</td>
                        <td>{symbol.exchange}</td>
                        <td>{symbol.exg_real}</td>
                        <td>{symbol.market}</td>
                        <td>{symbol.symbol}</td>
                        <td>{symbol.combined ? m.yes() : ''}</td>
                        <td>{fmtDateStr(symbol.list_ms)}</td>
                        <td>{fmtDateStr(symbol.delist_ms)}</td>
                        <td>
                            <a href={localizeHref(`/data/item?id=${symbol.id}`)} class="btn btn-xs btn-info btn-outline">{m.details()}</a>
                        </td>
                    </tr>
                {/each}
            </tbody>
        </table>
    </div>

    {@render pagination(totalCount, pageSize, currentPage, i => {currentPage = i}, i => {pageSize = i})}
</div>

<DrawerDataTools bind:show={isDrawerOpen} />
