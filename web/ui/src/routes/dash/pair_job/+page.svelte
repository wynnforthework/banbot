<script lang="ts">
  import { onMount } from 'svelte';
  import * as m from '$lib/paraglide/messages';
  import { getAccApi } from '$lib/netio';
  import Modal from '$lib/kline/Modal.svelte';

  interface StgyVer {
    name: string;
    version: number;
  }

  interface PairStgyTf {
    pair: string;
    strategy: string;
    tf: string;
    price: number;
    odNum: number;
    args: Array<{title: string, value: any}>;
  }

  let tabName = $state('symbol');
  let jobs = $state<PairStgyTf[]>([]);
  let stratList = $state<StgyVer[]>([]);
  let showCode = $state(false);
  let activeStgy = $state('');
  let stgyContent = $state('');
  let showJobEdit = $state(false);
  let pageSize = $state(20);
  let currentPage = $state(1);
  
  let job = $state<PairStgyTf>({
    pair: '',
    strategy: '',
    tf: '',
    price: 0,
    odNum: 0,
    args: []
  });

  async function loadData() {
    const rsp = await getAccApi('/stg_jobs');
    if(rsp.code === 200) {
      jobs = rsp.jobs ?? [];
      const strats: Record<string, number> = rsp.strats ?? {};
      stratList = Object.entries(strats).map(([name, version]) => ({
        name,
        version
      }));
    }
  }

  function editJobArgs(row: PairStgyTf) {
    job = {...row};
    showJobEdit = true;
  }

  function showJobArgs(row: PairStgyTf) {
    if(!Array.isArray(row.args)) return '';
    return row.args
      .filter(item => item.value)
      .map(item => item.title)
      .join(', ');
  }

  function getPaginatedData(data: any[]) {
    const start = (currentPage - 1) * pageSize;
    const end = start + pageSize;
    return data.slice(start, end);
  }

  function getTotalPages(total: number) {
    return Math.ceil(total / pageSize);
  }

  function handlePageChange(page: number) {
    const totalPages = getTotalPages(tabName === 'symbol' ? jobs.length : stratList.length);
    if (page >= 1 && page <= totalPages) {
      currentPage = page;
    }
  }

  onMount(() => {
    loadData();
  });
</script>

<Modal title={activeStgy} bind:show={showCode}>
  <pre class="overflow-auto max-h-[63vh] h-[800px]">{stgyContent}</pre>
</Modal>

<!-- Job Edit Modal will be implemented separately -->
<div class="bg-base-100 rounded-lg shadow-lg p-6 min-h-[600px]">
  <div class="w-full">
    <!-- Tab Menu -->
    <div class="flex justify-between items-center mb-4">
      <div class="tabs tabs-boxed">
        <button 
          class="tab {tabName === 'symbol' ? 'tab-active' : ''}"
          onclick={() => tabName = 'symbol'}
        >
          {m.pair()}
        </button>
        <button 
          class="tab {tabName === 'strategy' ? 'tab-active' : ''}"
          onclick={() => tabName = 'strategy'}
        >
          {m.strategy()}
        </button>
      </div>
      
      <select 
        class="select select-bordered w-full max-w-28"
        bind:value={pageSize}
        onchange={() => currentPage = 1}
      >
        <option value={10}>10 / {m.page()}</option>
        <option value={20}>20 / {m.page()}</option>
        <option value={30}>30 / {m.page()}</option>
        <option value={50}>50 / {m.page()}</option>
        <option value={100}>100 / {m.page()}</option>
      </select>
    </div>

    <!-- Symbol Table -->
    {#if tabName === 'symbol'}
      <div class="overflow-x-auto">
        <table class="table">
          <thead>
            <tr>
              <th>{m.pair()}</th>
              <th>{m.timeframe()}</th>
              <th>{m.price()}</th>
              <th>{m.order_num()}</th>
              <th>{m.strategy()}</th>
              <th>{m.settings()}</th>
              <th>{m.actions()}</th>
            </tr>
          </thead>
          <tbody>
            {#each getPaginatedData(jobs) as job}
              <tr>
                <td>{job.pair}</td>
                <td>{job.tf}</td>
                <td>{job.price}</td>
                <td>{job.odNum}</td>
                <td>{job.strategy}</td>
                <td>{showJobArgs(job)}</td>
                <td>
                  {#if Array.isArray(job.args)}
                    <button class="btn btn-xs btn-primary" 
                            onclick={() => editJobArgs(job)}>
                      {m.edit()}
                    </button>
                  {/if}
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    
    <!-- Strategy Table -->
    {:else if tabName === 'strategy'}
      <div class="overflow-x-auto">
        <table class="table">
          <thead>
            <tr>
              <th>{m.strategy()}</th>
              <th>{m.version()}</th>
            </tr>
          </thead>
          <tbody>
            {#each getPaginatedData(stratList) as stgy}
              <tr>
                <td>{stgy.name}</td>
                <td>{stgy.version}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}

    <!-- 分页控制 -->
    {#if (tabName === 'symbol' ? jobs : stratList).length > 0}
      <div class="flex justify-end mt-4">
        <div class="join">
          {#each Array(getTotalPages(tabName === 'symbol' ? jobs.length : stratList.length)) as _, i}
            <button 
              class="join-item btn {currentPage === i + 1 ? 'btn-active' : ''}"
              onclick={() => handlePageChange(i + 1)}
            >
              {i + 1}
            </button>
          {/each}
        </div>
      </div>
    {/if}
  </div> 
</div>