<script lang="ts" module>
	import type { Node, Tree, MenuItem, MenuClickEvent } from './types';
	import Icon from '../Icon.svelte';

	function traverseNodes(tree: Tree, nodes: string[], parentNodeId?: string) {
		if(!nodes) return;
		nodes.forEach((nodeId) => {
			const node: Node | undefined = tree[nodeId];
			if (!node) return;

			node.parentNodeId = parentNodeId;
			if (node.children?.length) {
				traverseNodes(tree, node.children, node.id);
			}
		});
	}
</script>

<script lang="ts">
	import TreeNode from './TreeNode.svelte';

	let { 
		tree, click,
		collapseControlled=false, 
		treeClass='', 
		treeNodeClass='', 
		childrenContainerClass='', 
		childPaddingLeft='0.7rem',
		active='',
		onMenu,
		onMenuClick
	}: {
		tree: Tree,
		collapseControlled?: boolean,
		treeClass?: string,
		treeNodeClass?: string,
		childrenContainerClass?: string,
		childPaddingLeft?: string,
		active?: string,
		click?: (event: {node: Node, collapsed: boolean}) => void,
		onMenu?: (node: Node) => MenuItem[],
		onMenuClick?: (event: MenuClickEvent) => void,
	} = $props();

	let activeMenuId = $state('');
	let showMenu = $state(false);
	let menuX = $state(0);
	let menuY = $state(0);
	let menuItems: MenuItem[] = $state([]);

	traverseNodes(tree, tree.children);

	function handleNodeClick(event: { node: Node; collapsed: boolean }) {
		const { node, collapsed } = event;

		if (!collapseControlled) {
			tree[node.id].collapsed = collapsed;
		}
		activeMenuId = '';
		click?.(event);
	}

	function handleOnMenu(node: Node): MenuItem[] {
		activeMenuId = node.id;
		return onMenu?.(node) ?? [];
	}

	function handleContextMenu(e: MouseEvent) {
		if (e.target === e.currentTarget) {
			e.preventDefault();
			activeMenuId = 'root';
			
			menuItems = onMenu?.({ id: 'root', name: '', type: 'container' }) ?? [];
			if (menuItems.length) {
				menuX = e.clientX;
				menuY = e.clientY;
				showMenu = true;
			}
		}
	}

	function handleMenuClick(event: MouseEvent, key: string) {
		event.stopPropagation();
		showMenu = false;
		onMenuClick?.({ node: { id: 'root', name: '', type: 'container' }, key });
	}

	function handleWindowClick() {
		showMenu = false;
	}
</script>

<svelte:window onclick={handleWindowClick} />

<div 
	class={'tree-view flex-1 ' + treeClass} 
	oncontextmenu={handleContextMenu}
>
	{#each tree.children as nodeId (nodeId)}
	{#if tree[nodeId]}
		<TreeNode
			{tree}
			node={tree[nodeId]}
			{active}
			{activeMenuId}
			{collapseControlled}
			{treeNodeClass}
			{childrenContainerClass}
			{childPaddingLeft}
			onMenu={handleOnMenu}
			{onMenuClick}
			click={handleNodeClick}
		/>
	{/if}
	{/each}
</div>

{#if showMenu}
	<div 
		class="context-menu px-4" 
		style="left: {menuX}px; top: {menuY}px"
	>
		{#each menuItems as item}
			<div 
				class="menu-item" 
				onclick={(e) => handleMenuClick(e, item.key)}
			>
				{#if item.icon}
					<div class="menu-icon">
						<Icon name={item.icon} />
					</div>
				{/if}
				<span>{item.text}</span>
			</div>
		{/each}
	</div>
{/if}

<style>
	.context-menu {
		position: fixed;
		background: hsl(var(--b1));
		border-radius: var(--rounded-lg, 0.5rem);
		padding: 0.25rem;
		min-width: 14rem;
		box-shadow: var(--shadow-lg);
		z-index: 1000;
	}

	.menu-icon {
		width: 1rem;
		height: 1rem;
		display: flex;
		align-items: center;
		justify-content: center;
	}

	.menu-item {
		padding: 0.375rem 0.75rem;
		display: flex;
		align-items: center;
		gap: 0.5rem;
		cursor: pointer;
		border-radius: var(--rounded-btn, 0.5rem);
		height: 1.75rem;
		font-size: 0.75rem;
		line-height: 1rem;
	}

	.menu-item:hover {
		background-color: hsl(var(--bc) / 0.1);
	}
</style>