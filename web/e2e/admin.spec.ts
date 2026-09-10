import {test,expect} from './fixtures';
import {execFileSync} from 'node:child_process';
import {tmpdir} from 'node:os';
import {join} from 'node:path';

test('question collection searches live and edits published answers without changing history',async({page,request},testInfo)=>{
 const title='Search lesson '+crypto.randomUUID();
 const upload=await request.post('/api/materials',{multipart:{title,kind:'notes',file:{name:'lesson.txt',mimeType:'text/plain',buffer:Buffer.from('Eu sunt acasă în fiecare zi.')}}});
 expect(upload.ok()).toBe(true);const {id}=await upload.json();
 expect((await request.post('/api/materials/'+id+'/generate',{data:{count:5,mode:'local'}})).ok()).toBe(true);
 const {exercises}=await(await request.get('/api/materials/'+id)).json();
 const question=exercises[0];
 expect((await request.post('/api/drafts/'+question.id+'/status',{data:{status:'published'}})).ok()).toBe(true);
 expect((await request.post('/api/attempts',{data:{id:crypto.randomUUID(),exerciseId:question.id,answer:question.answers[0]}})).ok()).toBe(true);
 await page.goto('/');await page.getByRole('button',{name:'Questions',exact:true}).click();
 await page.getByRole('searchbox',{name:'Search questions',exact:true}).fill(title);
 await expect(page.locator('.question-card')).toHaveCount(1);
 await expect(page.locator('.preview-answer')).toContainText('sunt');
 await expect(page.locator('.preview-options .correct-option')).toContainText('sunt');
 await page.getByRole('button',{name:'Edit question',exact:true}).click();
 await page.getByRole('combobox',{name:'Exercise type',exact:true}).selectOption('cloze');
 await page.getByRole('textbox',{name:'Question',exact:true}).fill('Use a fi in the present: Noi ____ acasă în fiecare zi.');
 await page.getByRole('textbox',{name:/^Accepted answers/}).fill('suntem');
 const explanation='Noi takes suntem, meaning we are.';
 await page.getByRole('textbox',{name:'Explanation',exact:true}).fill(explanation);
 await page.getByRole('button',{name:'Save changes',exact:true}).click();
 await expect(page.locator('.preview-answer')).toContainText('suntem');
 await expect(page.locator('.preview-explanation')).toHaveText(explanation);
 const result=await(await request.post('/api/attempts',{data:{id:crypto.randomUUID(),exerciseId:question.id,answer:'suntem'}})).json();
 expect(result.correct).toBe(true);
 const history=await(await request.get('/api/history')).json();
 expect(history.find((item:{answer:string})=>item.answer==='sunt').explanation).toBe(question.explanation);
 // Search updates without an Enter key or submit button, including answers.
 await page.getByRole('searchbox',{name:'Search questions',exact:true}).fill('suntem');
 await expect(page.locator('.question-card').filter({hasText:title})).toHaveCount(1);
 await page.getByRole('searchbox',{name:'Search questions',exact:true}).fill('no-matches-'+crypto.randomUUID());
 await expect(page.getByRole('heading',{name:'No questions found.'})).toBeVisible();
 await page.getByRole('searchbox',{name:'Search questions',exact:true}).fill(title);
 await page.getByRole('combobox',{name:'Status',exact:true}).selectOption('draft');
 await expect(page.getByRole('heading',{name:'No questions found.'})).toBeVisible();
 await page.getByRole('combobox',{name:'Status',exact:true}).selectOption('published');
 await expect(page.locator('.question-card')).toHaveCount(1);
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:join(tmpdir(),`romana-questions-${testInfo.project.name}.png`),fullPage:true});
});

test('admins can grant and revoke roles and learners cannot manage content',async({page,request,browser,baseURL})=>{
 const other=JSON.parse(execFileSync('python3',['scripts/microk8s.py','test-session'],{cwd:'..',encoding:'utf8'}));
 const learner=await browser.newContext();
 try{
  await learner.addCookies([{name:'romana_session',value:other.cookie.split('=')[1],url:baseURL!,httpOnly:true,sameSite:'Lax'}]);
  expect((await request.patch('/api/admin/users/'+other.userID,{data:{isAdmin:false}})).ok()).toBe(true);
  const learnerPage=await learner.newPage();await learnerPage.goto(baseURL!);
  await expect(learnerPage.getByRole('button',{name:'History',exact:true})).toBeVisible();
  for(const name of ['Course library','Questions','Users','Reports'])await expect(learnerPage.getByRole('button',{name,exact:true})).toHaveCount(0);
  expect((await learner.request.get(baseURL+'/api/admin/questions')).status()).toBe(403);
  expect((await learner.request.post(baseURL+'/api/materials',{data:{}})).status()).toBe(403);
  await page.goto('/');await page.getByRole('button',{name:'Users',exact:true}).click();
  await page.getByRole('searchbox',{name:'Search users',exact:true}).fill(other.userID);
  await expect(page.locator('.user-list li')).toHaveCount(1);
  await page.getByRole('button',{name:'Make admin',exact:true}).click();
  await expect(page.getByRole('button',{name:'Remove admin',exact:true})).toBeVisible();
  expect((await learner.request.get(baseURL+'/api/admin/questions')).ok()).toBe(true);
  await page.getByRole('button',{name:'Remove admin',exact:true}).click();
  await expect(page.getByRole('button',{name:'Make admin',exact:true})).toBeVisible();
  expect((await learner.request.get(baseURL+'/api/admin/questions')).status()).toBe(403);
 }finally{
  await learner.close();
  execFileSync('python3',['scripts/microk8s.py','cleanup-test'],{cwd:'..',input:JSON.stringify({userID:other.userID})});
 }
});
