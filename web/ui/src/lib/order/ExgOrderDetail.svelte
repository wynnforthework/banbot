<script lang="ts">
  import { fmtDateStrTZ } from '$lib/dateutil';
  import * as m from '$lib/paraglide/messages';
  import type { BanExgOrder, BanTrade } from '$lib/order/types';
  import Modal from '../kline/Modal.svelte';

  let { 
    show = $bindable(false),
    order = undefined,
  } = $props();
  
  // 将order设置为响应式状态
  let currentOrder = $state<BanExgOrder | null>(null);
  
  // 监听order变化
  $effect(() => {
    currentOrder = order;
  });

  // 获取状态对应的颜色类
  function getStatusColor(status: string): string {
    switch(status.toLowerCase()) {
      case 'closed': return "bg-success/20 text-success border-success/30"; 
      case 'canceled': return "bg-error/20 text-error border-error/30"; 
      case 'open': return "bg-primary/20 text-primary border-primary/30"; 
      default: return "bg-base-200 text-base-content border-base-300"; 
    }
  }
  
  // 获取方向对应的颜色类
  function getSideColor(side: string): string {
    return side.toLowerCase() === 'sell' ? "text-error" : "text-success";
  }

  // 复制内容到剪贴板
  function copyToClipboard(text: string): void {
    navigator.clipboard.writeText(text);
    // 可以添加一个toast提示，这里略过
  }
</script>

<Modal title={m.order_details()} bind:show={show} width={1200}>
  {#if currentOrder}
  <div class="flex flex-col gap-2 max-h-[80vh] overflow-y-auto p-1 pr-2 custom-scrollbar">
    <!-- 订单概览卡片 -->
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <!-- 卡片头部 -->
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-primary/20 p-1.5 rounded-full">
            <span class="i-lucide-briefcase text-primary text-sm"></span>
          </div>
          <div class="flex flex-row gap-0.5">
            <span class="text-md font-bold">{m.order_summary()}</span>
            <span class="ml-2 flex items-center gap-1 text-xs text-base-content/70">
              <span class="font-mono">{currentOrder.symbol}</span>
              <span>•</span>
              <span class={getSideColor(currentOrder.side)}>
                {currentOrder.side}
              </span>
              <span>•</span>
              <span class="font-mono">{currentOrder.type}</span>
            </span>
          </div>
        </div>
        
        <div class="flex items-center gap-2">
          <div class={`px-2 py-0.5 text-xs rounded-full border ${getStatusColor(currentOrder.status)}`}>
            {currentOrder.status}
          </div>
        </div>
      </div>
      
      <!-- 卡片内容 -->
      <div class="p-3 grid grid-cols-1 md:grid-cols-3 gap-3">
        <!-- 基本信息 -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold flex items-center gap-1 mb-1">
            <span class="i-lucide-info text-primary text-xs"></span>
            {m.basic_info()}
          </h3>
          
          <div class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-xs">
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.order_id()}</div>
              <div class="font-mono flex items-center gap-1">
                <span class="truncate">{currentOrder?.id}</span>
                <button onclick={() => currentOrder?.id && copyToClipboard(currentOrder.id)} 
                        class="btn btn-xs btn-ghost btn-square p-0 h-4 min-h-0 w-4" 
                        title={m.copy_id()}>
                  <span class="i-lucide-copy text-base-content/50 text-xs"></span>
                </button>
              </div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.client_order_id()}</div>
              <div class="font-mono flex items-center gap-1">
                <span class="truncate">{currentOrder?.clientOrderId}</span>
                <button onclick={() => currentOrder?.clientOrderId && copyToClipboard(currentOrder.clientOrderId)} 
                        class="btn btn-xs btn-ghost btn-square p-0 h-4 min-h-0 w-4" 
                        title={m.copy_id()}>
                  <span class="i-lucide-copy text-base-content/50 text-xs"></span>
                </button>
              </div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.time_in_force()}</div>
              <div class="font-mono">{currentOrder.timeInForce}</div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.position_side()}</div>
              <div class="font-mono">{currentOrder.positionSide || '-'}</div>
            </div>

            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.create_time()}</div>
              <div class="font-mono text-[10px]">{fmtDateStrTZ(currentOrder.timestamp)}</div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.last_update()}</div>
              <div class="font-mono text-[10px]">{fmtDateStrTZ(currentOrder.lastUpdateTimestamp)}</div>
            </div>
          </div>
        </div>
        
        <!-- 价格数量信息 -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold flex items-center gap-1 mb-1">
            <span class="i-lucide-trending-up text-primary text-xs"></span>
            {m.price_amount()}
          </h3>
          
          <div class="grid grid-cols-2 gap-2">
            <!-- 价格卡片 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.price?.toFixed(7) ?? '-'}
              </span>
            </div>
            
            <!-- 实际成交均价 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.average_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.average?.toFixed(7) ?? '-'}
              </span>
            </div>
            
            <!-- 数量 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.amount()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.amount?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 已成交数量 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.filled_amount()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.filled?.toFixed(5) ?? '-'}
              </span>
            </div>

            <!-- 成本 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.cost()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.cost?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 剩余数量 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.remaining()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.remaining?.toFixed(5) ?? '-'}
              </span>
            </div>
          </div>
        </div>
        
        <!-- 其他条件 -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold flex items-center gap-1 mb-1">
            <span class="i-lucide-settings text-primary text-xs"></span>
            {m.conditions()}
          </h3>
          
          <div class="grid grid-cols-2 gap-2">
            <!-- 触发价格 -->
            {#if currentOrder.triggerPrice}
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.trigger_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.triggerPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            {/if}
            
            <!-- 止损价格 -->
            {#if currentOrder.stopPrice}
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.stop_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.stopPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            {/if}
            
            <!-- 止盈价格 -->
            {#if currentOrder.takeProfitPrice}
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.take_profit_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.takeProfitPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            {/if}
            
            <!-- 止损价格 -->
            {#if currentOrder.stopLossPrice}
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.stop_loss_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.stopLossPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            {/if}
            
            <!-- 仅平仓 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.reduce_only()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.reduceOnly ? m.yes() : m.no()}
              </span>
            </div>
            
            <!-- 仅挂单 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.post_only()}</span>
              <span class="text-sm font-medium font-mono">
                {currentOrder.postOnly ? m.yes() : m.no()}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  
    <!-- 手续费信息 -->
    {#if currentOrder.fee}
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-warning/20 p-1.5 rounded-full">
            <span class="i-lucide-receipt text-warning text-sm"></span>
          </div>
          <h2 class="text-sm font-bold">{m.fee_info()}</h2>
        </div>
      </div>
      
      <div class="p-3 grid grid-cols-2 md:grid-cols-4 gap-x-3 gap-y-2 text-xs">
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.fee_amount()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentOrder.fee.cost?.toFixed(7) ?? '-'}
          </div>
        </div>
        
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.fee_currency()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentOrder.fee.currency ?? '-'}
          </div>
        </div>
        
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.fee_rate()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentOrder.fee.rate?.toFixed(5) ?? '-'}
          </div>
        </div>
        
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.is_maker()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentOrder.fee.isMaker ? m.yes() : m.no()}
          </div>
        </div>
      </div>
    </div>
    {/if}

    <!-- 成交记录 -->
    {#if currentOrder.trades && currentOrder.trades.length > 0}
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-info/20 p-1.5 rounded-full">
            <span class="i-lucide-list-ordered text-info text-sm"></span>
          </div>
          <h2 class="text-sm font-bold">{m.trades_list()}</h2>
        </div>
        
        <div class="badge badge-sm">
          {currentOrder.trades.length}
        </div>
      </div>
      
      <div class="overflow-x-auto">
        <table class="table table-xs table-zebra w-full">
          <thead>
            <tr>
              <th>{m.trade_id()}</th>
              <th>{m.trade_time()}</th>
              <th>{m.price()}</th>
              <th>{m.amount()}</th>
              <th>{m.cost()}</th>
              <th>{m.side()}</th>
              <th>{m.is_maker()}</th>
              <th>{m.fee_amount()}</th>
              <th>{m.fee_currency()}</th>
            </tr>
          </thead>
          <tbody>
            {#each currentOrder.trades as trade}
            <tr>
              <td class="font-mono text-xs">{trade.id}</td>
              <td class="font-mono text-xs">{fmtDateStrTZ(trade.timestamp, 'MM-DD HH:mm:ss')}</td>
              <td class="font-mono text-xs">{trade.price?.toFixed(7) ?? '-'}</td>
              <td class="font-mono text-xs">{trade.amount?.toFixed(5) ?? '-'}</td>
              <td class="font-mono text-xs">{trade.cost?.toFixed(5) ?? '-'}</td>
              <td class={`font-mono text-xs ${getSideColor(trade.side)}`}>{trade.side}</td>
              <td class="font-mono text-xs">{trade.maker ? m.yes() : m.no()}</td>
              <td class="font-mono text-xs">{trade.fee?.cost?.toFixed(7) ?? '-'}</td>
              <td class="font-mono text-xs">{trade.fee?.currency ?? '-'}</td>
            </tr>
            {/each}
          </tbody>
        </table>
      </div>
    </div>
    {/if}
  
    <!-- 额外信息 -->
    {#if currentOrder.info}
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-primary/20 p-1.5 rounded-full">
            <span class="i-lucide-file-text text-primary text-sm"></span>
          </div>
          <h2 class="text-sm font-bold">{m.additional_info()}</h2>
        </div>
      </div>
      
      <div class="p-2">
        <pre class="whitespace-pre-wrap break-all bg-base-200 p-2 rounded text-xs font-mono overflow-x-auto border border-base-300">{JSON.stringify(currentOrder.info, null, 2)}</pre>
      </div>
    </div>
    {/if}
  </div> 
  {/if}
</Modal>

<style>
  /* 自定义滚动条样式 */
  .custom-scrollbar::-webkit-scrollbar {
    width: 5px;
    height: 5px;
  }
  
  .custom-scrollbar::-webkit-scrollbar-thumb {
    background-color: rgba(0, 0, 0, 0.2);
    border-radius: 3px;
  }
  
  .custom-scrollbar::-webkit-scrollbar-track {
    background-color: transparent;
  }
  
  .custom-scrollbar:hover::-webkit-scrollbar-thumb {
    background-color: rgba(0, 0, 0, 0.3);
  }
</style>
