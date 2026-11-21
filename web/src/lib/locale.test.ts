import { describe, expect, it } from 'vitest';
import { cookieName, extractLocaleFromRequest } from '#lib/paraglide/runtime.js';

describe('default app language', () => {
	it.each(['de-DE,de;q=0.9', 'nl-BE,nl;q=0.9'])('serves English for %s', (language) => {
		const request = new Request('http://localhost/login', {
			headers: { 'Accept-Language': language }
		});
		expect(extractLocaleFromRequest(request)).toBe('en');
	});

	it('ignores an old cookie for an inactive translation', () => {
		const request = new Request('http://localhost/login', {
			headers: { Cookie: `${cookieName}=de` }
		});
		expect(extractLocaleFromRequest(request)).toBe('en');
	});
});
