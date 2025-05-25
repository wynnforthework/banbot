<script lang="ts">
  import { fmtDateStrTZ } from '$lib/dateutil';
  import * as m from '$lib/paraglide/messages';
  import type { Position } from '$lib/order/types';
  import Modal from '../kline/Modal.svelte';

  let { 
    show = $bindable(false),
    pos = undefined,
  } = $props();
  
  // 将 pos 设置为可响应的状态
  let currentPos = $state<Position | null>(null);
  
  // 监听 pos 变化
  $effect(() => {
    currentPos = pos;
  });

  // 获取side对应的颜色类
  function getSideColor(side: string): string {
    return side.toLowerCase() === 'long' ? "text-success" : "text-error";
  }
  
  // 获取盈亏对应的颜色类
  function getProfitColor(value: number | undefined): string {
    if (value === undefined) return "";
    return value > 0 ? "text-success font-medium" : value < 0 ? "text-error font-medium" : "";
  }

  // 复制ID到剪贴板
  function copyToClipboard(text: string) {
    navigator.clipboard.writeText(text);
    // 可以添加一个toast提示，这里略过
  }

  // 格式化百分比
  function formatPercent(value: number | undefined): string {
    if (value === undefined) return "-";
    return (value * 100).toFixed(2) + "%";
  }
</script>

<Modal title={m.position_details()} bind:show={show} width={1000}>
  {#if currentPos}
  <div class="flex flex-col gap-2 max-h-[80vh] overflow-y-auto p-1 pr-2 custom-scrollbar">
    <!-- 仓位概览卡片 -->
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <!-- 卡片头部 -->
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-primary/20 p-1.5 rounded-full">
            <span class="i-lucide-briefcase text-primary text-sm"></span>
          </div>
          <div class="flex flex-row gap-0.5">
            <span class="text-md font-bold">{m.position_summary()}</span>
            <span class="ml-2 flex items-center gap-1 text-xs text-base-content/70">
              <span class="font-mono">{currentPos.symbol}</span>
              <span>•</span>
              <span class={currentPos.side.toLowerCase() === 'long' ? "text-success" : "text-error"}>
                {currentPos.side.toLowerCase() === 'long' ? m.long() : m.short()}
              </span>
            </span>
          </div>
        </div>
        
        <div class="flex items-center gap-2">
          <div class="text-right">
            <span class="text-xs text-base-content/70">{m.leverage()}</span>
            <span class="text-sm font-bold">{currentPos.leverage}x</span>
          </div>
          
          <div class={`px-2 py-0.5 text-xs rounded-full border ${currentPos.side.toLowerCase() === 'long' ? "bg-success/20 text-success border-success/30" : "bg-error/20 text-error border-error/30"}`}>
            {currentPos.marginMode}
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
              <div class="text-xs text-base-content/70">ID</div>
              <div class="font-mono flex items-center gap-1">
                <span class="truncate">{currentPos?.id}</span>
                <button onclick={() => currentPos?.id && copyToClipboard(currentPos.id.toString())} 
                        class="btn btn-xs btn-ghost btn-square p-0 h-4 min-h-0 w-4" 
                        title={m.copy_id()}>
                  <span class="i-lucide-copy text-base-content/50 text-xs"></span>
                </button>
              </div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.update_time()}</div>
              <div class="font-mono text-[10px]">{fmtDateStrTZ(currentPos.timestamp)}</div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.margin_mode()}</div>
              <div class="font-mono truncate">{currentPos.marginMode}</div>
            </div>
            
            <div class="space-y-0.5">
              <div class="text-xs text-base-content/70">{m.isolated()}</div>
              <div class="font-mono">{currentPos.isolated ? m.yes() : m.no()}</div>
            </div>
          </div>
        </div>
        
        <!-- 价格数据 -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold flex items-center gap-1 mb-1">
            <span class="i-lucide-trending-up text-primary text-xs"></span>
            {m.price_amount()}
          </h3>
          
          <div class="grid grid-cols-2 gap-2">
            <!-- 入场价格卡片 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.entry_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.entryPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            
            <!-- 标记价格卡片 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.mark_price()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.markPrice?.toFixed(7) ?? '-'}
              </span>
            </div>
            
            <!-- 数量 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.contracts()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.contracts?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 合约大小 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.contract_size()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.contractSize?.toFixed(5) ?? '-'}
              </span>
            </div>
          </div>
        </div>
        
        <!-- 保证金和盈亏 -->
        <div class="space-y-2">
          <h3 class="text-sm font-semibold flex items-center gap-1 mb-1">
            <span class="i-lucide-bar-chart-3 text-primary text-xs"></span>
            {m.margin_pnl()}
          </h3>
          
          <div class="grid grid-cols-2 gap-2">
            <!-- 名义价值 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.notional()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.notional?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 未实现盈亏 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.unrealized_pnl()}</span>
              <span class={`text-sm font-medium font-mono ${getProfitColor(currentPos.unrealizedPnl)}`}>
                {currentPos.unrealizedPnl?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 初始保证金 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.initial_margin()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.initialMargin?.toFixed(5) ?? '-'}
              </span>
            </div>
            
            <!-- 维持保证金 -->
            <div class="bg-base-200/30 rounded p-1.5 border border-base-300 flex flex-col">
              <span class="text-xs text-base-content/70">{m.maintenance_margin()}</span>
              <span class="text-sm font-medium font-mono">
                {currentPos.maintenanceMargin?.toFixed(5) ?? '-'}
              </span>
            </div>
          </div>
        </div>
      </div>
    </div>
  
    <!-- 保证金详情 -->
    <div class="bg-base-100 rounded-lg shadow-sm border border-base-200">
      <div class="flex items-center justify-between px-3 py-2 bg-base-200/50 border-b border-base-300">
        <div class="flex items-center gap-2">
          <div class="bg-warning/20 p-1.5 rounded-full">
            <span class="i-lucide-percent text-warning text-sm"></span>
          </div>
          <h2 class="text-sm font-bold">{m.margin_details()}</h2>
        </div>
      </div>
      
      <div class="p-3 grid grid-cols-2 md:grid-cols-4 gap-x-3 gap-y-2 text-xs">
        <!-- 保证金率 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.margin_ratio()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {formatPercent(currentPos.marginRatio)}
          </div>
        </div>
        
        <!-- 初始保证金比例 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.initial_margin_percentage()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {formatPercent(currentPos.initialMarginPercentage)}
          </div>
        </div>
        
        <!-- 维持保证金比例 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.maintenance_margin_percentage()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {formatPercent(currentPos.maintenanceMarginPercentage)}
          </div>
        </div>
        
        <!-- 百分比变化 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.percentage_change()}</div>
          <div class={`bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono ${getProfitColor(currentPos.percentage)}`}>
            {formatPercent(currentPos.percentage)}
          </div>
        </div>
        
        <!-- 抵押品 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.collateral()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentPos.collateral?.toFixed(5) ?? '-'}
          </div>
        </div>
        
        <!-- 清算价格 -->
        <div class="space-y-0.5">
          <div class="text-xs text-base-content/70">{m.liquidation_price()}</div>
          <div class="bg-base-200/30 p-1.5 rounded border border-base-300 text-xs font-mono">
            {currentPos.liquidationPrice?.toFixed(7) ?? '-'}
          </div>
        </div>
      </div>
    </div>
  
    <!-- 额外信息 -->
    {#if currentPos.info}
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
        <pre class="whitespace-pre-wrap break-all bg-base-200 p-2 rounded text-xs font-mono overflow-x-auto border border-base-300">{JSON.stringify(currentPos.info, null, 2)}</pre>
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
