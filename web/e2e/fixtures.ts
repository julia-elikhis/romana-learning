import {test as base, expect} from '@playwright/test';
import {execFileSync} from 'node:child_process';

// Provision isolated sessions through database administration, not an HTTP login bypass.
// Capture the token privately; never log it or persist browser storage state.
export const test=base.extend<{account:void}>({
 account:[async({context,baseURL},use)=>{
  const fixture=JSON.parse(execFileSync('python3',['scripts/microk8s.py','test-session'],{cwd:'..',encoding:'utf8'}));
  try{
   await context.addCookies([{name:'romana_session',value:fixture.cookie.split('=')[1],url:baseURL!,httpOnly:true,sameSite:'Lax'}]);
   await use();
  }finally{
   execFileSync('python3',['scripts/microk8s.py','cleanup-test'],{cwd:'..',input:JSON.stringify({userID:fixture.userID})});
  }
 },{auto:true}],
 request:async({context},use)=>{await use(context.request)}
});
export {expect};
