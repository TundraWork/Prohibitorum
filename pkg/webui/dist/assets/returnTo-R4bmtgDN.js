function i(n){if(!n)return null;try{const o=new URL(n,window.location.origin);return o.origin===window.location.origin?o.toString():null}catch{return null}}export{i as s};
