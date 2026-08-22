// Personal Edition owns a deliberately small route surface.
//
// The upstream SaaS router is intentionally not retained on personal-v1: even
// when selected only at runtime, its dynamic imports are still discovered by
// vue-tsc/Vite and would keep subscription, registration, monitoring
// and other SaaS-only screens in the Personal dependency graph.
//
// Keep the canonical Personal router in one place and make the conventional
// router entry point re-export it so existing imports continue to work.
export { default } from './personal'
