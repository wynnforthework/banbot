<script lang="ts">
  import { getDateStr } from '$lib/dateutil';
  import * as m from '$lib/paraglide/messages';
  import type { Order } from '$lib/dev/common';

  export let order: Order;
  export let editable = false;
</script>

<div class="space-y-4">
  <!-- 基本信息 -->
  <div class="grid grid-cols-2 gap-4 p-4 border rounded-lg">
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">ID</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" bind:value={order.id} />
        {:else}
          <div class="p-2">{order.id}</div>
        {/if}
      </div>
    </div>
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.pair()}</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" bind:value={order.symbol} />
        {:else}
          <div class="p-2">{order.symbol}</div>
        {/if}
      </div>
    </div>
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.timeframe()}</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" bind:value={order.timeframe} />
        {:else}
          <div class="p-2">{order.timeframe}</div>
        {/if}
      </div>
    </div>
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.side()}</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" value={order.short ? m.short() : m.long()} />
        {:else}
          <div class="p-2">{order.short ? m.short() : m.long()}</div>
        {/if}
      </div>
    </div>
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.leverage()}</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" value={`${order.leverage}x`} />
        {:else}
          <div class="p-2">{order.leverage}x</div>
        {/if}
      </div>
    </div>
    <div class="form-control">
      <div class="flex items-center gap-4">
        <label class="label w-24 whitespace-nowrap">{m.strategy()}</label>
        {#if editable}
          <input type="text" class="input input-bordered flex-1" value={`${order.strategy}:${order.stg_ver}`} />
        {:else}
          <div class="p-2">{order.strategy}:{order.stg_ver}</div>
        {/if}
      </div>
    </div>
  </div>

  <!-- 入场订单信息 -->
  <div class="border rounded-lg p-4">
    <h3 class="text-lg font-bold mb-4">{m.enter_order()}</h3>
    <div class="grid grid-cols-2 gap-4">
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.enter_time()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" value={getDateStr(order.enter_at)} />
          {:else}
            <div class="p-2">{getDateStr(order.enter_at)}</div>
          {/if}
        </div>
      </div>
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.enter_tag()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" bind:value={order.enter_tag} />
          {:else}
            <div class="p-2">{order.enter_tag}</div>
          {/if}
        </div>
      </div>
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.init_price()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" value={order.init_price?.toFixed(7)} />
          {:else}
            <div class="p-2">{order.init_price?.toFixed(7)}</div>
          {/if}
        </div>
      </div>
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.enter_price()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" value={order.enter_price?.toFixed(7)} />
          {:else}
            <div class="p-2">{order.enter_price?.toFixed(7)}</div>
          {/if}
        </div>
      </div>
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.average_price()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" value={order.enter_average?.toFixed(7)} />
          {:else}
            <div class="p-2">{order.enter_average?.toFixed(7)}</div>
          {/if}
        </div>
      </div>
      <div class="form-control">
        <div class="flex items-center gap-4">
          <label class="label w-24 whitespace-nowrap">{m.amount()}</label>
          {#if editable}
            <input type="text" class="input input-bordered flex-1" value={order.enter_amount?.toFixed(5)} />
          {:else}
            <div class="p-2">{order.enter_amount?.toFixed(5)}</div>
          {/if}
        </div>
      </div>
    </div>
  </div>

  <!-- 出场订单信息 -->
  {#if order.exit_tag}
    <div class="border rounded-lg p-4">
      <h3 class="text-lg font-bold mb-4">{m.exit_order()}</h3>
      <div class="grid grid-cols-2 gap-4">
        <div class="form-control">
          <div class="flex items-center gap-4">
            <label class="label w-24 whitespace-nowrap">{m.exit_time()}</label>
            {#if editable}
              <input type="text" class="input input-bordered flex-1" value={getDateStr(order.exit_at)} />
            {:else}
              <div class="p-2">{getDateStr(order.exit_at)}</div>
            {/if}
          </div>
        </div>
        <div class="form-control">
          <div class="flex items-center gap-4">
            <label class="label w-24 whitespace-nowrap">{m.exit_tag()}</label>
            {#if editable}
              <input type="text" class="input input-bordered flex-1" bind:value={order.exit_tag} />
            {:else}
              <div class="p-2">{order.exit_tag}</div>
            {/if}
          </div>
        </div>
        {#if order.exit_filled}
          <div class="form-control">
            <div class="flex items-center gap-4">
              <label class="label w-24 whitespace-nowrap">{m.exit_price()}</label>
              {#if editable}
                <input type="text" class="input input-bordered flex-1" value={order.exit_price?.toFixed(7)} />
              {:else}
                <div class="p-2">{order.exit_price?.toFixed(7)}</div>
              {/if}
            </div>
          </div>
          <div class="form-control">
            <div class="flex items-center gap-4">
              <label class="label w-24 whitespace-nowrap">{m.average_price()}</label>
              {#if editable}
                <input type="text" class="input input-bordered flex-1" value={order.exit_average?.toFixed(7)} />
              {:else}
                <div class="p-2">{order.exit_average?.toFixed(7)}</div>
              {/if}
            </div>
          </div>
          <div class="form-control">
            <div class="flex items-center gap-4">
              <label class="label w-24 whitespace-nowrap">{m.amount()}</label>
              {#if editable}
                <input type="text" class="input input-bordered flex-1" value={order.exit_amount?.toFixed(5)} />
              {:else}
                <div class="p-2">{order.exit_amount?.toFixed(5)}</div>
              {/if}
            </div>
          </div>
          <div class="form-control">
            <div class="flex items-center gap-4">
              <label class="label w-24 whitespace-nowrap">{m.filled_amount()}</label>
              {#if editable}
                <input type="text" class="input input-bordered flex-1" value={order.exit_filled?.toFixed(5)} />
              {:else}
                <div class="p-2">{order.exit_filled?.toFixed(5)}</div>
              {/if}
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}

  <!-- 额外信息 -->
  {#if order.info}
    <div class="border rounded-lg p-4">
      <h3 class="text-lg font-bold mb-4">{m.additional_info()}</h3>
      <pre class="whitespace-pre-wrap bg-base-200 p-4 rounded-lg">{JSON.stringify(order.info, null, 2)}</pre>
    </div>
  {/if}
</div> 