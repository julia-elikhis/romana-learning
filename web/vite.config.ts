import { defineConfig } from 'vite';
export default defineConfig({server:{proxy:{'/api':'http://127.0.0.1:8080','/readyz':'http://127.0.0.1:8080'}}});
