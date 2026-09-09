import { defineConfig, devices } from '@playwright/test';
export default defineConfig({
 testDir:'./e2e',workers:1,
 use:{baseURL:'http://127.0.0.1:8080',channel:'chrome'},
 projects:[
  {name:'desktop',use:{viewport:{width:1440,height:1000}}},
  {name:'mobile',use:{...devices['Pixel 7'],defaultBrowserType:'chromium',channel:'chrome'}}
 ]
});
