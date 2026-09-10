import {test,expect} from './fixtures';
import {type APIRequestContext,type Page} from '@playwright/test';
import {join} from 'node:path';
import {tmpdir} from 'node:os';

async function lesson(request:APIRequestContext,page:Page){
 const upload=await request.post('/api/materials',{multipart:{title:'Practice review '+crypto.randomUUID(),kind:'notes',file:{name:'lesson.txt',mimeType:'text/plain',buffer:Buffer.from('Eu sunt acasă în fiecare zi.\nNoi avem o casă foarte frumoasă.\nTu mergi la școală dimineața.')}}});
 expect(upload.ok()).toBe(true);const {id}=await upload.json();
 expect((await request.post('/api/materials/'+id+'/generate',{data:{count:5,mode:'local'}})).ok()).toBe(true);
 const {exercises}=await(await request.get('/api/materials/'+id)).json();
 for(const q of exercises){
  expect((await request.patch('/api/exercises/'+q.id,{data:{kind:'cloze',prompt:q.prompt,answers:q.answers,options:[],explanation:q.explanation}})).ok()).toBe(true);
  expect((await request.post('/api/drafts/'+q.id+'/status',{data:{status:'published'}})).ok()).toBe(true);
 }
 // Keep browser writes within this test's course. Selection still runs through
 // the real backend and saved history, including current/guest-answer hints.
 await page.route('**/api/practice/question*',route=>{
  const url=new URL(route.request().url());url.searchParams.set('materialId',id);
  return route.continue({url:url.toString()});
 });
 return {id,exercises};
}
const nextResponse=(page:Page)=>page.waitForResponse(r=>new URL(r.url()).pathname==='/api/practice/question');

test('practice starts immediately, shuffles unanswered questions, and colors feedback',async({page,request},testInfo)=>{
 const {exercises}=await lesson(request,page);
 let response=nextResponse(page);await page.goto('/');const first=await(await response).json();
 await expect(page.locator('.exercise-prompt')).toHaveText(first.prompt);
 await expect(page.locator('.practice-intro')).toHaveText('YOUR PRACTICE SPACEA little today.More confidence tomorrow.');
 await expect(page.locator('.stats')).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Start a little practice'})).toHaveCount(0);
 await expect(page.locator('.account-chip .avatar')).toBeVisible();
 await expect(page.locator('.account-chip').getByRole('button',{name:'Sign out',exact:true})).toBeVisible();
 response=nextResponse(page);await page.getByRole('button',{name:'Shuffle question',exact:true}).click();const shuffled=await(await response).json();
 expect(shuffled.id).not.toBe(first.id);
 expect(await(await request.get('/api/history')).json()).toHaveLength(0);
 await page.getByLabel('Your answer',{exact:true}).fill('incorrect');
 await page.getByRole('button',{name:'Check answer',exact:true}).click();
 await expect(page.locator('.feedback-incorrect')).toContainText('Not quite.');
 const incorrectColor=await page.locator('.feedback-incorrect').evaluate(el=>getComputedStyle(el).backgroundColor);
 await expect(page.locator('.practice-card blockquote')).toHaveCount(0);
 await expect(page.getByText('Lesson evidence',{exact:true})).toHaveCount(0);
 await page.screenshot({path:join(tmpdir(),`romana-instant-practice-${testInfo.project.name}.png`),fullPage:true});
 response=nextResponse(page);await page.getByRole('button',{name:'Next question',exact:true}).click();const next=await(await response).json();
 expect(next.id).not.toBe(shuffled.id);
 await page.getByLabel('Your answer',{exact:true}).fill(exercises.find((q:{id:string})=>q.id===next.id).answers[0]);
 await page.getByRole('button',{name:'Check answer',exact:true}).click();
 await expect(page.locator('.feedback-correct')).toContainText('Nicely done.');
 expect(await page.locator('.feedback-correct').evaluate(el=>getComputedStyle(el).backgroundColor)).not.toBe(incorrectColor);
 await expect(page.locator('.practice-card blockquote')).toHaveCount(0);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
});

test('guests can report a question and admins can correct and resolve it',async({page,request,browser,baseURL},testInfo)=>{
 const {id}=await lesson(request,page);
 const guest=await browser.newContext();
 try{
  const guestPage=await guest.newPage();
  await guestPage.route('**/api/practice/question*',route=>{const url=new URL(route.request().url());url.searchParams.set('materialId',id);return route.continue({url:url.toString()})});
  const response=nextResponse(guestPage);await guestPage.goto(baseURL!);const question=await(await response).json();
  await expect(guestPage.getByRole('button',{name:'Reports',exact:true})).toHaveCount(0);
  expect((await guest.request.get(baseURL+'/api/admin/reports')).status()).toBe(401);
  await guestPage.getByRole('button',{name:'Report question',exact:true}).click();
  const note='The answer should be suntem. '+crypto.randomUUID();
  await guestPage.getByRole('textbox',{name:/What seems wrong/}).fill(note);
  // Retrying after a lost response must not create a second report.
  await guestPage.route('**/api/exercises/*/reports',async route=>{const sent=await route.fetch();expect(sent.ok()).toBe(true);await route.fulfill({status:503,json:{error:'Connection interrupted. Please retry.'}})},{times:1});
  await guestPage.getByRole('button',{name:'Send report',exact:true}).click();
  await expect(guestPage.getByRole('alert')).toContainText('Connection interrupted.');
  await guestPage.getByRole('button',{name:'Send report',exact:true}).click();
  await expect(guestPage.getByText('Report sent. An admin will review this question.')).toBeVisible();
  await expect(guestPage.getByLabel('Your answer',{exact:true})).toBeEnabled();
  await page.goto('/');await page.getByRole('button',{name:'Reports',exact:true}).click();
  const card=page.locator('.report-card').filter({hasText:note});
  await expect(card).toHaveCount(1);await expect(card).toContainText('Anonymous learner');
  await card.getByRole('button',{name:'Edit question',exact:true}).click();
  await card.getByRole('textbox',{name:'Question',exact:true}).fill('Complete with a fi: Noi ____ acasă.');
  await card.getByRole('textbox',{name:/^Accepted answers/}).fill('suntem');
  await card.getByRole('textbox',{name:'Explanation',exact:true}).fill('Noi suntem means we are.');
  await card.getByRole('button',{name:'Save changes',exact:true}).click();
  await expect(card.locator('.preview-answer')).toContainText('suntem');
  const graded=await(await guest.request.post(baseURL+'/api/attempts',{data:{id:crypto.randomUUID(),exerciseId:question.id,answer:'suntem'}})).json();
  expect(graded.correct).toBe(true);expect(graded.saved).toBe(false);
  await card.getByRole('button',{name:'Mark resolved',exact:true}).click();
  await expect(card).toHaveCount(0);
  await page.getByRole('combobox',{name:'Report status',exact:true}).selectOption('resolved');
  await expect(card).toHaveCount(1);
  await expect(card.getByRole('button',{name:'Reopen report',exact:true})).toBeVisible();
  expect(await(await request.get('/api/history')).json()).toHaveLength(0);
  expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
  await page.screenshot({path:join(tmpdir(),`romana-report-review-${testInfo.project.name}.png`),fullPage:true});
 }finally{await guest.close()}
});

test('guest question memory lasts only for the visit and loading failures can be retried',async({page,context,request})=>{
 await lesson(request,page);await context.clearCookies();
 let response=nextResponse(page);await page.goto('/');const first=await(await response).json();
 await page.getByLabel('Your answer',{exact:true}).fill('incorrect');
 await page.getByRole('button',{name:'Check answer',exact:true}).click();
 await expect(page.getByText('Anonymous practice · answer not saved',{exact:true})).toBeVisible();
 response=nextResponse(page);await page.getByRole('button',{name:'Next question',exact:true}).click();
 const next=await response;expect((await next.json()).id).not.toBe(first.id);
 expect(new URL(next.request().url()).searchParams.getAll('answeredId')).toEqual([first.id]);
 response=nextResponse(page);await page.reload();
 expect(new URL((await response).request().url()).searchParams.has('answeredId')).toBe(false);
 await expect(page.locator('.exercise-prompt')).toBeVisible();
 await page.route('**/api/practice/question*',route=>route.fulfill({status:503,json:{error:'No connection. Please retry.'}}),{times:1});
 await page.getByRole('button',{name:'Shuffle question',exact:true}).click();
 await expect(page.getByRole('alert')).toContainText('No connection.');
 await page.getByRole('button',{name:'Retry connection',exact:true}).click();
 await expect(page.getByRole('alert')).toHaveCount(0);
 await expect(page.locator('.exercise-prompt')).toBeVisible();
});
