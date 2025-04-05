import tailwindcss from "@tailwindcss/vite";
import { paraglideVitePlugin } from '@inlang/paraglide-js'
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

export default defineConfig({
	plugins: [
		tailwindcss(),
		paraglideVitePlugin({
			project: './project.inlang',
			outdir: './src/lib/paraglide',
			strategy: ['url', 'cookie', 'baseLocale'],
			urlPatterns: [
				{
					pattern: "/:path(.*)?",
					localized: [
						["zh-CN", "/zh-CN/:path(.*)?"],
						["en-US", "/en-US/:path(.*)?"],
					],
				},
			],
		}),
		sveltekit()
	],
	resolve: {
    alias: {
      '@': '/src',
			'klinecharts': 'klinecharts/dist/index.esm.js'
		}
	},
	optimizeDeps: {
		exclude: ["codemirror", "@codemirror/lang-go"],
	}
});