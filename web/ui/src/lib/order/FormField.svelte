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

<fieldset class="fieldset">
  {#if editable}
    <label class="label flex items-center gap-4">
      <span class="w-28 whitespace-nowrap text-sm">{label}</span>
      <input type="text" class="input input-sm w-48" bind:value={value}/>
    </label>
  {:else}
    <label class="label flex items-center gap-4">
      <span class="w-28 whitespace-nowrap text-sm">{label}</span>
      <span class="p-2 text-sm">{value}</span>
    </label>
  {/if}
</fieldset> 