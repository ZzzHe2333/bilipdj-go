package core

import (
 "net/http"
 "net/http/httptest"
 "testing"

 "github.com/ZzzHe2333/bilipdj-go/internal/live"
)

func TestNumericNicknameBlacklistNeverBecomesOtherPlatformUserIDBan(t *testing.T){
 dir:=t.TempDir()
 a:=New(dir,"test","repo")
 a.config.Permissions=[]PermissionEntry{}
 if !a.upsertRoleLocked("douyin","123456","blacklist"){t.Fatal("ban rejected")}
 // Previous versions mirrored all scoped nicknames into the old global list.
 if !named(a.config.Blacklist,"123456"){t.Fatal("expected legacy mirror")}
 unrelated:=live.Event{Platform:"bilibili",Username:"OtherUser",UserID:"123456"}
 if a.isBlacklisted(unrelated){t.Fatal("Douyin numeric nickname erroneously blocked another platform's Bilibili UID")}
 if !a.isBlacklisted(live.Event{Platform:"douyin",Username:"123456",UserID:"not-123456"}){
  t.Fatal("actual Douyin nickname ban not enforced")
 }
 if a.isBlacklisted(live.Event{Platform:"douyin",Username:"OtherUser",UserID:"123456"}){
  t.Fatal("nickname ban should not block unrelated Douyin user ID")
 }
 if err:=a.saveLocked();err!=nil{t.Fatal(err)}
 reloaded:=New(dir,"test","repo")
 if reloaded.isBlacklisted(unrelated){t.Fatal("cross-platform numeric ban reappeared after restart")}
 if !reloaded.isBlacklisted(live.Event{Platform:"douyin",Username:"123456",UserID:"different"}){
  t.Fatal("scoped ban not persisted")
 }
}

func TestMultipleScopedNicknameBansRetainMirrorUntilAllRemoved(t *testing.T){
 a:=New(t.TempDir(),"test","repo")
 a.config.Permissions=[]PermissionEntry{}
 for _,platform:=range []string{"bilibili","douyin"}{
  if !a.upsertRoleLocked(platform,"20261013","blacklist"){t.Fatal("unable to ban",platform)}
 }
 if !a.removeRoleLocked("bilibili","20261013","blacklist"){t.Fatal("unable to remove Bilibili scoped ban")}
 if !named(a.config.Blacklist,"20261013"){t.Fatal("removing Bilibili ban erased Douyin legacy mirror")}
 if a.isBlacklisted(live.Event{Platform:"bilibili",UserID:"20261013",Username:"Unrelated"}){
  t.Fatal("surviving Douyin mirror became a Bilibili UID ban")
 }
 if !a.isBlacklisted(live.Event{Platform:"douyin",UserID:"xyz",Username:"20261013"}){
  t.Fatal("Douyin ban was erased")
 }
 if !a.removeRoleLocked("douyin","20261013","blacklist"){t.Fatal("unable to remove remaining Douyin ban")}
 if named(a.config.Blacklist,"20261013"){t.Fatal("removed all scopes but left a stale legacy mirror")}
}

func TestPermissionsAPIRemovalPreservesOtherPlatformBlacklist(t *testing.T){
 a:=New(t.TempDir(),"test","repo")
 a.config.Permissions=[]PermissionEntry{}
 a.upsertRoleLocked("bilibili","87654321","blacklist")
 a.upsertRoleLocked("douyin","87654321","blacklist")
 response:=consoleRequest(t,a,"POST","/api/permissions",map[string]any{"entries":[]PermissionEntry{
  {Name:"87654321",Platform:"douyin",Role:"blacklist"},
 }})
 if response.Code!=200{t.Fatalf("save permissions failed: %d %s",response.Code,response.Body.String())}
 if !named(a.config.Blacklist,"87654321"){t.Fatal("removing one scope erased remaining mirrored ban")}
 if !a.isBlacklisted(live.Event{Platform:"douyin",Username:"87654321",UserID:"user"}){
  t.Fatal("remaining Douyin blacklist rule missing")
 }
 if a.isBlacklisted(live.Event{Platform:"bilibili",Username:"Different",UserID:"87654321"}){
  t.Fatal("numeric nickname treated as UID across platforms")
 }
}

func TestLegacyUserIDBansRemainEffectiveAcrossPermissionMigration(t *testing.T){
 for _,migrated:=range []bool{false,true}{
  a:=New(t.TempDir(),"test","repo")
  a.config.Blacklist=[]string{"13579"}
  if migrated{a.config.Permissions=legacyPermissions(a.config)}
  e:=live.Event{Platform:"bilibili",Username:"AnotherPerson",UserID:"13579"}
  if !a.isBlacklisted(e){t.Fatalf("legacy ID ban lost after migration=%v",migrated)}
  if a.commandAllowed(e,"queue"){t.Fatal("banned user acquired command privileges")}
 }
}

func TestAdminLoopbackIPv4IPv6AndRemoteIsolation(t *testing.T){
 t.Setenv("BILIPDJ_ADMIN_TOKEN","")
 a:=New(t.TempDir(),"test","repo")
 for _,tc:=range []struct{host,remote,origin string;want int}{
  {"localhost:9816","127.0.0.1:54001","",200},
  {"127.0.0.1:9816","127.0.0.1:54001","",200},
  {"[::1]:9816","[::1]:54001","",200},
  {"[::1]:9816","[::1]:54001","http://[::1]:9816",200},
  {"[::1]","[::1]:54001","",200},
  {"[::1]:9816","192.0.2.35:54001","",403},
  {"localhost:9816","192.0.2.35:54001","",403},
  {"example.com:9816","127.0.0.1:54001","",403},
  {"[::1]:9816","[::1]:54001","https://evil.example",403},
 }{
  req:=httptest.NewRequest(http.MethodGet,"http://"+tc.host+"/api/config",nil)
  req.Host=tc.host
  req.RemoteAddr=tc.remote
  if tc.origin!=""{req.Header.Set("Origin",tc.origin)}
  w:=httptest.NewRecorder()
  a.Routes(http.NotFoundHandler()).ServeHTTP(w,req)
  if w.Code!=tc.want{t.Errorf("host=%s remote=%s origin=%s: got=%d want=%d, body=%s",tc.host,tc.remote,tc.origin,w.Code,tc.want,w.Body.String())}
 }
}
