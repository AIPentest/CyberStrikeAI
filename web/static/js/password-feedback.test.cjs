const fs=require('node:fs');
const vm=require('node:vm');
const test=require('node:test');
const assert=require('node:assert/strict');
const auth=fs.readFileSync('web/static/js/auth.js','utf8');
const settings=fs.readFileSync('web/static/js/settings.js','utf8');
function input(id,value='') {
 const attrs={};
 return {id,value,type:'password',style:{},classList:{toggle(){},remove(){}},focus(){this.focused=true;},setAttribute(k,v){attrs[k]=v;},getAttribute:k => attrs[k]};
}
function harness(values={}) {
 const fields={};
 for (const id of ['auth-current-password','auth-new-password','auth-confirm-password','login-username','login-password']) fields[id]=input(id,values[id]||'');
 for (const id of ['auth-current-password-error','auth-new-password-error','auth-confirm-password-error','password-form-error','login-error','login-password-toggle']) fields[id]=input(id);
 const button={disabled:false};let calls=0;
 const ctx={document:{getElementById:id => fields[id]||null,querySelector:() => button},window:{t:x => x},alert(){},console:{error(){}},
  fetch:async () => {calls++;throw Error('unexpected login request');},
  apiFetch:async () => {calls++;return {ok:false,json:async () => ({error:'test-only incorrect current password'})};}};
 vm.createContext(ctx);
 let start=auth.indexOf('function setLoginPasswordVisible(');
 vm.runInContext(auth.slice(start,auth.indexOf('function showLoginOverlay(',start)),ctx);
 start=auth.indexOf('function clearLoginFormError(');
 vm.runInContext(auth.slice(start,auth.indexOf('\nfunction ',start)),ctx);
 start=settings.indexOf('function setPasswordFieldError(');
 vm.runInContext(settings.slice(start,settings.indexOf('// ==================== 外部MCP管理',start)),ctx);
 return {ctx,fields,button,calls:() => calls};
}
test('empty login password explains the error and never calls the server',async () => {
 const h=harness({'login-username':'admin'});
 await h.ctx.submitLogin({preventDefault(){}});
 assert.equal(h.calls(),0);
 assert.equal(h.fields['login-error'].textContent,'auth.enterPassword');
 assert.equal(h.fields['login-password'].getAttribute('aria-invalid'),'true');
 assert.equal(h.fields['login-password'].focused,true);
 h.ctx.clearLoginFormError({target:h.fields['login-password']});
 assert.equal(h.fields['login-error'].style.display,'none');
 assert.equal(h.fields['login-password'].getAttribute('aria-invalid'),'false');
 assert.match(fs.readFileSync('web/templates/index.html','utf8'),/<form id="login-form"[^>]*novalidate/);
});
test('password preview toggles accessibly and can be reset without changing the value',() => {
 const h=harness({'login-password':'test-only-password'});
 h.ctx.toggleLoginPasswordVisibility();
 assert.equal(h.fields['login-password'].type,'text');
 assert.equal(h.fields['login-password-toggle'].getAttribute('aria-pressed'),'true');
 assert.equal(h.fields['login-password-toggle'].getAttribute('aria-label'),'login.hidePassword');
 assert.equal(h.fields['login-password-toggle'].getAttribute('title'),'login.hidePassword');
 h.ctx.setLoginPasswordVisible(false);
 assert.equal(h.fields['login-password'].type,'password');
 assert.equal(h.fields['login-password'].value,'test-only-password');
 assert.equal(h.fields['login-password-toggle'].getAttribute('data-i18n'),'login.showPassword');
 assert.equal(h.fields['login-password-toggle'].getAttribute('aria-label'),'login.showPassword');
 assert.equal(h.fields['login-password-toggle'].getAttribute('title'),'login.showPassword');
 assert.equal(h.fields['login-password-toggle'].textContent,undefined);
 h.ctx.window.t=undefined;
 h.ctx.toggleLoginPasswordVisibility();
 assert.equal(h.fields['login-password-toggle'].getAttribute('aria-label'),'Hide password');
});
test('password form has field-specific errors without requesting a save',async () => {
 const h=harness({'auth-new-password':'short','auth-confirm-password':'different'});
 await h.ctx.changePassword();
 assert.equal(h.calls(),0);
 assert.equal(h.fields['auth-current-password-error'].textContent,'settingsSecurity.currentRequired');
 assert.equal(h.fields['auth-new-password-error'].textContent,'settingsSecurity.newTooShort');
 assert.equal(h.fields['auth-confirm-password-error'].textContent,'settingsSecurity.confirmMismatch');
 assert.equal(h.fields['auth-current-password'].focused,true);
 h.ctx.resetPasswordForm();
 for (const id of ['auth-current-password','auth-new-password','auth-confirm-password']) {
  assert.equal(h.fields[id].getAttribute('aria-invalid'),'false');
  assert.equal(h.fields[id+'-error'].hidden,true);
 }
});
test('server password failure is visible in the form and restores the save button',async () => {
 const h=harness({'auth-current-password':'test-only-old','auth-new-password':'test-only-new','auth-confirm-password':'test-only-new'});
 await h.ctx.changePassword();
 assert.equal(h.calls(),1);
 assert.equal(h.fields['password-form-error'].hidden,false);
 assert.equal(h.fields['password-form-error'].textContent,'test-only incorrect current password');
 assert.equal(h.button.disabled,false);
});
