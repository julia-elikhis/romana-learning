import {test,expect} from './fixtures';
import {join} from 'node:path';
import {tmpdir} from 'node:os';

const entries=[
 {rank:1,login:'ana-learner',score:12,isYou:false},
 {rank:2,login:'mihai-practices',score:10,isYou:false},
 {rank:3,login:'ioana-learns',score:8,isYou:false},
 {rank:4,login:'dan-study',score:6,isYou:false},
 {rank:5,login:'elena-romanian',score:5,isYou:false},
];
const dates={weekStart:'2026-09-07T00:00:00Z',weekEnd:'2026-09-14T00:00:00Z'};

test('leaderboard shows top learners and own rank, refreshing after a saved answer',async({page,request},testInfo)=>{
 let score=2;
 await page.route('**/api/leaderboard',route=>route.fulfill({json:{entries,you:{rank:6,login:'my-romanian-practice',score,isYou:true},participants:7,...dates}}));
 const deckResponse=page.waitForResponse(r=>new URL(r.url()).pathname==='/api/practice/question');
 await page.goto('/');const deck=await(await deckResponse).json();
 const widget=page.getByRole('region',{name:'Leaderboard',exact:true});
 await expect(widget.getByRole('listitem')).toHaveCount(6);
 await expect(widget.getByText('ana-learner',{exact:true})).toBeVisible();
 await expect(widget.getByText('You',{exact:true})).toBeVisible();
 await expect(widget.locator('.leaderboard-own .leaderboard-score strong')).toHaveText('2');
 await expect(widget.getByText(/counted once a week/)).toBeVisible();
 const mission=page.getByRole('region',{name:'Practice mission'});
 const missionBox=await mission.boundingBox(),widgetBox=await widget.boundingBox();
 if(page.viewportSize()!.width<=800)expect(widgetBox!.y).toBeGreaterThan(missionBox!.y+missionBox!.height);
 else {expect(widgetBox!.x).toBeLessThan(missionBox!.x);expect(Math.abs(widgetBox!.y-missionBox!.y)).toBeLessThan(2)}
 const questions=await(await request.get('/api/admin/questions')).json();
 const answers=questions.questions.find((q:{id:string})=>q.id===deck.id).answers;
 const answer=answers[0];
 const typed=page.getByLabel('Your answer',{exact:true});
 if(await typed.count())await typed.fill(answer);else if(deck.kind==='multi_select'){for(const value of answers)await page.getByRole('checkbox',{name:value,exact:true}).check()}else await page.locator('.options').getByRole('button',{name:answer,exact:true}).click();
 score=3;
 await page.getByRole('button',{name:'Check answer',exact:true}).click();
 await expect(page.getByText('Saved to your history',{exact:true})).toBeVisible();
 await expect(widget.locator('.leaderboard-own .leaderboard-score strong')).toHaveText('3');
 expect(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth)).toBe(true);
 await page.screenshot({path:join(tmpdir(),`romana-leaderboard-${testInfo.project.name}.png`),fullPage:true});
});

test('guest leaderboard handles errors and an empty week without blocking practice',async({page,context})=>{
 await context.clearCookies();
 let unavailable=true;
 await page.route('**/api/leaderboard',route=>route.fulfill(unavailable?{status:503,json:{error:'Temporarily unavailable'}}:{json:{entries:[],you:null,participants:0,...dates}}));
 await page.goto('/');
 const widget=page.getByRole('region',{name:'Leaderboard',exact:true});
 await expect(widget.getByText('The leaderboard is unavailable right now.')).toBeVisible();
 await expect(page.getByRole('button',{name:'Shuffle question',exact:true})).toBeEnabled();
 unavailable=false;
 await widget.getByRole('button',{name:'Retry leaderboard'}).click();
 await expect(widget.getByText('A fresh week starts here.')).toBeVisible();
 await expect(widget.getByRole('link',{name:'Sign in to join the leaderboard'})).toHaveAttribute('href','/auth/github');
 await expect(widget.getByRole('listitem')).toHaveCount(0);
 await expect(page.getByRole('button',{name:'Shuffle question',exact:true})).toBeEnabled();
});
