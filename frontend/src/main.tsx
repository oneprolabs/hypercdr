import React from "react";
import { createRoot } from "react-dom/client";
import App from "./App";
import type { HyperCDRFrontendModule } from "./app/extensions";
import { Building2, Users, KeyRound, Globe } from 'lucide-react';
import CommunityMigrationAuthorizationsPage from './features/migrations/community-migration-authorizations-page';
import "./styles.css";
import { PublicAuthPage, SaaSLoginSupplement } from './features/saas/saas-auth';
import { PersonalInformation, OrganizationInformation } from './features/saas/organization-settings';
import { AccountAccess } from './features/saas/account-access';
import { AccessLoginPage } from './features/saas/access-login-page';
import { PlatformDirectory } from './features/saas/platform-directory';
import { MembersPage } from './features/saas/members-page';

import { VerifyBoundEmail } from './features/saas/bound-email';

const modules:HyperCDRFrontendModule[]=[
{id:'platform-users',view:'extension:platform-users',navigation:{label:'Users',description:'All platform users and organization ownership',icon:Users,order:60,group:'settings'},component:props=><PlatformDirectory {...props} kind="users"/>,isVisible:({currentUser})=>currentUser.systemAdmin===true},
{id:'platform-organizations',view:'extension:platform-organizations',navigation:{label:'Organizations',description:'All organizations and their members',icon:Building2,order:70,group:'settings'},component:props=><PlatformDirectory {...props} kind="organizations"/>,isVisible:({currentUser})=>currentUser.systemAdmin===true},
{id:'platform-access',view:'extension:platform-access',navigation:{label:'Access & Sign-in',description:'Platform access address and sign-in links',icon:Globe,order:100,group:'settings'},component:AccessLoginPage,isVisible:({currentUser})=>currentUser.systemAdmin===true},
{id:'signin-security',view:'extension:signin-security',navigation:{label:'Sign-in & Security',description:'Password and Google sign-in',icon:KeyRound,order:20,group:'settings'},component:AccountAccess},
{id:'organization',view:'extension:organization',navigation:{label:'Organization',description:'Your organization information',icon:Building2,order:30,group:'settings'},component:OrganizationInformation,isVisible:({currentUser})=>!currentUser.systemAdmin},
{id:'members',view:'extension:members',navigation:{label:'Members',description:'Organization members and invitations',icon:Users,order:40,group:'settings'},component:MembersPage,isVisible:({currentUser})=>!currentUser.systemAdmin},{id:'community-migration',view:'extension:community-migration',navigation:{label:'Upgrade to Enterprise',description:'Authorize and monitor Community migration',icon:Building2,order:130,group:'settings'},component:({toast})=><CommunityMigrationAuthorizationsPage toast={toast}/>,isVisible:()=>false /* Temporarily hide the Enterprise upgrade entry. */}];
createRoot(document.getElementById("root") as HTMLElement).render(
  <React.StrictMode>
    {window.location.pathname==='/verify-email'?<VerifyBoundEmail/>:['/signup','/join','/auth/complete'].includes(window.location.pathname)?<PublicAuthPage/>:<App modules={modules} profileComponent={PersonalInformation} profileLabel="Personal Information" loginSupplement={<SaaSLoginSupplement/>}/>}
  </React.StrictMode>
);
