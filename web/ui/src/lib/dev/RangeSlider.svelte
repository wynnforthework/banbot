<script lang="ts">
    import { onMount } from 'svelte';

    let canvas: HTMLCanvasElement;
    let container: HTMLDivElement;
    let slider: HTMLDivElement;
    let isDragging = false;
    let dragType: 'left' | 'right' | 'middle' = 'middle';
    let startX = 0;
    let initialStart = 0;
    let initialEnd = 0;

    // 逻辑宽度1000，start,end都是逻辑位置
    // 实际显示过程会根据data的长度计算显示
    let {
        start=$bindable(0),
        end=$bindable(100),
        data=[],
        height=25,
        isBar=false,
        class: className = '',
        change,
    }: {
        start: number,
        end: number,
        data?: number[],
        height?: number,
        isBar?: boolean,
        class?: string,
        change?: (start: number, end: number) => void
    } = $props();

    let refresh = $state(0);
    let containerWidth = $derived.by(() => {
        // 使用 refresh 触发重新计算
        refresh;
        return container?.clientWidth || 0;
    });
    let sliderLeft = $derived((start / 1000) * containerWidth);
    let sliderWidth = $derived(((end - start) / 1000) * containerWidth); 

    function drawChart() {
        if (!canvas) return;
        const ctx = canvas.getContext('2d');
        if (!ctx) return;

        const width = canvas.width;
        const height = canvas.height;
        ctx.clearRect(0, 0, width, height);

        if (data.length === 0) return;

        const max = Math.max(...data);
        const min = Math.min(...data);
        const range = max - min;

        ctx.beginPath();
        ctx.moveTo(0, height - ((data[0] - min) / range) * height);

        if (isBar) {
            const barWidth = width / data.length;
            data.forEach((value, index) => {
                const x = index * barWidth;
                const y = height - ((value - min) / range) * height;
                ctx.fillStyle = 'rgba(135, 206, 235, 0.6)';
                ctx.fillRect(x, y, barWidth, height - y);
            });
        } else {
            // 绘制折线图
            data.forEach((value, index) => {
                const x = (index / (data.length - 1)) * width;
                const y = height - ((value - min) / range) * height;
                ctx.lineTo(x, y);
            });

            // 填充区域
            ctx.lineTo(width, height);
            ctx.lineTo(0, height);
            ctx.closePath();
            ctx.fillStyle = 'rgba(135, 206, 235, 0.2)';
            ctx.fill();

            // 绘制线条
            ctx.strokeStyle = 'rgb(30, 144, 255)';
            ctx.lineWidth = 2;
            ctx.stroke();
        }
    }

    function handleMouseDown(e: MouseEvent) {
        const rect = slider.getBoundingClientRect();
        const clickX = e.clientX - rect.left;
        
        if (clickX <= 8) {
            dragType = 'left';
        } else if (clickX >= rect.width - 8) {
            dragType = 'right';
        } else {
            dragType = 'middle';
        }

        isDragging = true;
        startX = e.clientX;
        initialStart = start;
        initialEnd = end;
    }

    function handleMouseMove(e: MouseEvent) {
        if (!isDragging) return;

        const delta = e.clientX - startX;
        const step = 1000 / containerWidth;
        const movement = Math.round(delta * step);

        if (dragType === 'left') {
            let newStart = Math.max(0, Math.min(end - 1, initialStart + movement));
            start = newStart;
        } else if (dragType === 'right') {
            let newEnd = Math.max(start + 1, Math.min(1000, initialEnd + movement));
            end = newEnd;
        } else {
            const width = initialEnd - initialStart;
            let newStart = Math.max(0, Math.min(1000 - width, initialStart + movement));
            start = newStart;
            end = newStart + width;
        }
    }

    function handleMouseUp() {
        if (isDragging) {
            isDragging = false;
            change?.(start, end);
        }
    }

    onMount(() => {
        const resizeObserver = new ResizeObserver(() => {
            if (canvas && container) {
                canvas.width = container.clientWidth;
                canvas.height = height;
                drawChart();
            }
        });

        if (container) {
            resizeObserver.observe(container);
        }

        const intersectionObserver = new IntersectionObserver(entries => {
            entries.forEach(entry => {
                if (entry.isIntersecting) {
                    refresh += 1;
                    drawChart();
                }
            });
        });

        if (container) {
            intersectionObserver.observe(container);
        }

        return () => {
            resizeObserver.disconnect();
            intersectionObserver.disconnect();
        };
    });

    $effect(() => {
        if (canvas && data) {
            setTimeout(drawChart, 0);
        }
    })
</script>

<div 
    class="container {className}" 
    bind:this={container}
    style="height: {height}px;"
    onmousemove={handleMouseMove}
    onmouseup={handleMouseUp}
    onmouseleave={handleMouseUp}
>
    <canvas bind:this={canvas}></canvas>
    <div 
        class="slider"
        bind:this={slider}
        style="left: {sliderLeft}px; width: {sliderWidth}px;"
        onmousedown={handleMouseDown}
    >
        <div class="handle left"></div>
        <div class="handle right"></div>
    </div>
</div>

<style>
    .container {
        position: relative;
        width: 100%;
        background: #f5f5f5;
        border-radius: 4px;
        overflow: hidden;
    }

    canvas {
        position: absolute;
        top: 0;
        left: 0;
        width: 100%;
        height: 100%;
    }

    .slider {
        position: absolute;
        top: 0;
        height: 100%;
        background: rgba(30, 144, 255, 0.3);
        cursor: grab;
        touch-action: none;
    }

    .slider:active {
        cursor: grabbing;
    }

    .handle {
        position: absolute;
        top: 0;
        width: 8px;
        height: 100%;
        background: rgba(30, 144, 255, 0.5);
        cursor: ew-resize;
    }

    .handle:hover {
        background: rgba(30, 144, 255, 0.7);
    }

    .handle.left {
        left: 0;
        border-top-left-radius: 4px;
        border-bottom-left-radius: 4px;
    }

    .handle.right {
        right: 0;
        border-top-right-radius: 4px;
        border-bottom-right-radius: 4px;
    }
</style> 