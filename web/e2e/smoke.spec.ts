import { test, expect } from '@playwright/test';
import { execFileSync } from 'node:child_process';

test('practice saves progress and survives a browser reload',async({page,request},testInfo)=>{
 const ids:string[]=[];
 const before=await (await request.get('/api/progress')).json();
 const errors:string[]=[];
 page.on('pageerror',error=>errors.push(error.message));
 page.on('request',r=>{if(r.url().endsWith('/api/attempts')&&r.method()==='POST')ids.push(r.postDataJSON().id)});
 try{
  await page.goto('/');
  await expect(page.getByRole('heading',{name:'Make yourself at home'})).toBeVisible();
  await page.getByRole('button',{name:'Start a little practice'}).click();
  await page.getByRole('button',{name:'case',exact:true}).click();
  await page.getByRole('button',{name:'Check answer'}).click();
  await expect(page.getByText('Saved to Postgres',{exact:true})).toBeVisible();
  await expect(page.getByText('Nicely done.',{exact:true})).toBeVisible();
  await page.reload();
  await expect(page.locator('.stats strong').first()).toHaveText(String(before.attempts+1));
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=window.innerWidth)).toBe(true);
  expect(errors).toEqual([]);
  await page.screenshot({path:`/private/tmp/romanian-${testInfo.project.name}.png`,fullPage:true});
 }finally{
  for(const id of new Set(ids)){
   if(!/^[a-f0-9-]{36}$/.test(id))throw new Error('Unexpected test attempt ID');
   execFileSync('docker',['compose','exec','-T','postgres','psql','-U','romanian','-d','romanian','-v','ON_ERROR_STOP=1','-c',`DELETE FROM attempts WHERE id = '${id}'`],{cwd:'..'});
  }
 }
});
