import { defineEnvVars } from '@sveltejs/kit/env';
import brand from '../../internal/branding/brand.json';

export const variables = defineEnvVars({
	APP_NAME: { public: true, static: true, schema: () => brand.name },
	APP_DESCRIPTION: { public: true, static: true, schema: () => brand.description }
});
