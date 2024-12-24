<script lang="ts">
  let {
    label,
    value=$bindable(''),
    editable = false,
  }: {
    label: string;
    value: any;
    editable?: boolean;
  } = $props();

</script>

<div class="form-control">
  <div class="flex items-center gap-4">
    <label class="label w-28 whitespace-nowrap text-sm">{label}</label>
    {#if editable}
      <input type="text" class="input input-bordered input-sm flex-1" 
        bind:value={value}/>
    {:else}
      <div class="p-2 text-sm">{value}</div>
    {/if}
  </div>
</div> 