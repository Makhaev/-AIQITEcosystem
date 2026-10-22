import { defineConfig } from '@playwright/test';
export default defineConfig({
 testDir:'./e2e',testMatch:'r13.spec.ts',workers:1,fullyParallel:false,timeout:180_000,
 reporter:[['list'],['json',{outputFile:'../.cache/r13/browser-report.json'}]],
 outputDir:'../.cache/r13/browser-artifacts',
 use:{actionTimeout:15000,navigationTimeout:15000,baseURL:process.env.R13_BROWSER_ORIGIN??'http://127.0.0.1:14173',browserName:'chromium',viewport:{width:1440,height:1000},
 launchOptions:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH?{executablePath:process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH}:{},
 // A real scenario handles credentials. Do not retain network traces with bodies.
 trace:'off',screenshot:'only-on-failure'},
});
