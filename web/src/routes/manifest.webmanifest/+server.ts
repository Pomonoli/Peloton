import { APP_DESCRIPTION, APP_NAME } from '$app/env/public';
import { json } from '@sveltejs/kit';

export const prerender = true;

export function GET() {
	return json(
		{
			name: APP_NAME,
			short_name: APP_NAME,
			description: APP_DESCRIPTION,
			lang: 'en',
			start_url: '/',
			scope: '/',
			display: 'standalone',
			background_color: '#f2f6fc',
			theme_color: '#fbfdff',
			icons: [
				{ src: '/favicon-192x192.png', sizes: '192x192', type: 'image/png' },
				{ src: '/favicon-512x512.png', sizes: '512x512', type: 'image/png' },
				{
					src: '/favicon-512x512.png',
					sizes: '512x512',
					type: 'image/png',
					purpose: 'any maskable'
				},
				{ src: '/favicon.svg', sizes: 'any', type: 'image/svg+xml' }
			]
		},
		{ headers: { 'Content-Type': 'application/manifest+json; charset=utf-8' } }
	);
}
