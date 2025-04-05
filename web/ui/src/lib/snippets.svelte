<script lang="ts" module>
  import type {InOutOrder} from "$lib/order";
  import { fmtDateStr } from '$lib/dateutil';
  import * as m from '$lib/paraglide/messages.js'
  export {orderCard, pagination}

    
  function formatNumber(num: number, decimals = 2) {
    if(!num) return '0';
    if(decimals >= 6 && num > 1){
      decimals = 4;
    }
    return num.toFixed(decimals);
  }

  function formatPercent(num: number, decimals = 1) {
    if(!num) return '0%';
    return num.toFixed(decimals) + '%';
  }
</script>

{#snippet pagination(total: number, pageSize: number, currentPage: number, onPageChange: (newPage: number) => void, onPageSizeChange: (newSize: number) => void)}
  <div class="flex justify-between items-center">
    <div class="flex items-center gap-2">
      <span>{m.page_size()}: </span>
      <input type="number" class="input input-sm w-20" value={pageSize} onchange={e => onPageSizeChange(Number(e.currentTarget.value))} />
    </div>
    <div class="join">
      <button class="join-item btn btn-sm" disabled={currentPage === 1}
              onclick={() => onPageChange(currentPage - 1)}>
        {m.prev_page()}
      </button>
      {#if total > 0}
      <button class="join-item btn btn-disabled btn-sm">
        {currentPage} / {Math.ceil(total / pageSize)}
      </button>
      {/if}
      <button class="join-item btn btn-sm" disabled={total > 0 && currentPage >= Math.ceil(total / pageSize)}
              onclick={() => onPageChange(currentPage + 1)}>
        {m.next_page()}
      </button>
    </div>
  </div>
{/snippet}

{#snippet orderCard(order: InOutOrder, isSelected: boolean, onAnalysis: () => void, onDetail: (e: Event) => void)}
  <div class="w-[15em] mr-2 mb-3 bg-base-200 hover:bg-base-300 cursor-pointer shadow-sm hover:shadow-md transition-all rounded-lg"
    onclick={onAnalysis}
    class:bg-slate-200={isSelected}
  >
    <div class="p-3.5">
      <div class="flex mb-2.5 items-center justify-between">
        <h3 class="text-sm font-semibold">{order.symbol}</h3>
        <button class="btn btn-xs btn-ghost"
          onclick={onDetail}>{m.details()}</button>
      </div>

      <div class="flex justify-between mb-2 text-sm">
        <div class="tooltip opacity-75" data-tip={m.enter_tag()}>
          {order.enter_tag}
        </div>
        <div class="tooltip font-medium" data-tip={m.enter_price()}>
          {formatNumber(order.enter?.average||order.enter?.price || 0, 8)}
        </div>
        <div class="tooltip opacity-75" data-tip={m.enter_amount()}>
          {formatNumber(order.enter?.filled ||order.enter?.amount || 0, 8)}
        </div>
      </div>

      <div class="flex justify-between mb-2 text-sm">
        <div class="tooltip opacity-75" data-tip={m.exit_tag()}>
          {order.exit_tag}
        </div>
        <span class={`px-1.5 py-0.5 text-xs rounded ${order.short ? 'bg-red-100 text-red-700' : 'bg-green-100 text-green-700'}`}>
          {order.short ? m.short() : m.long()} {order.leverage}x
        </span>
        <div class="tooltip font-medium" data-tip={m.profit_rate()} class:text-success={order.profit > 0} class:text-error={order.profit <= 0}>
          {formatPercent(order.profit * 100 / ((order.enter?.filled || 0) * (order.enter?.average || 1)))}
        </div>
      </div>

      <div class="flex justify-between text-xs text-base-content/60">
        <div class="tooltip" data-tip={m.exit_time()}>
          {fmtDateStr(order.exit_at)}
        </div>
        <div class="tooltip" data-tip={m.strategy()}>
          {order.strategy}
        </div>
      </div>
    </div>
  </div>
{/snippet}