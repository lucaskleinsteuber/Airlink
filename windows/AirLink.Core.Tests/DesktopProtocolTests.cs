using AirLink.Core;
using System.Text.Json;
internal static class DesktopProtocolTests {
 public static Task Run() {
  void Require(bool ok,string message) { if (!ok) throw new InvalidOperationException(message); }
  var session = JsonSerializer.Deserialize<PairingSession>("""{"id":"attempt","state":"comparing","comparisonCode":"00001234","localConfirmed":false} """,ProtocolJson.Options)!;
  Require(session.CanConfirm && session.FormattedCode=="0000 1234","Leading-zero comparison code was lost");
  session.LocalConfirmed=true;Require(!session.CanConfirm,"Confirmed attempt could be confirmed again");
  session.LocalConfirmed=false;session.State="expired";Require(!session.CanConfirm,"Expired attempt allowed confirmation");
  var initial = new HostSettings {Name="Original",ReceiveDirectory=Path.GetTempPath()};
  var draft = new SettingsDraft(initial);draft.Values.Name="Edited";
  var latest=ProtocolJson.Clone(initial);latest.AutomaticUpdateChecks=true;draft.Rebase(latest);
  Require(draft.Values.Name=="Edited" && draft.Values.AutomaticUpdateChecks,"Rebase reset a deliberate name or failed to adopt an untouched update preference");
  draft.Values.AutomaticUpdateChecks=false;Require(!draft.Merge(latest).AutomaticUpdateChecks,"Explicit update opt-out was lost");
  var history=new[]{new Transfer{Id="old",DeviceId="chosen",State="completed"},new Transfer{Id="becomes-active",DeviceId="chosen",State="completed"}};
  var summary=new[]{new Transfer{Id="live",DeviceId="other",State="running"},new Transfer{Id="becomes-active",DeviceId="chosen",State="paused"}};
  var merged=DesktopTransferSelection.MergeHistory(history,summary);
  Require(merged.Count==3 && merged.Count(t=>t.IsActive)==2,"Paged history lost a live transfer or kept a stale state");
  Require(DesktopTransferSelection.ForDevice(merged,"chosen").All(t=>t.DeviceId=="chosen"),"Device view exposed another recipient's transfers");
  var sparse=new Transfer{Summary=true,Direction="receive",State="completed",HasContent=false,Files=[new TransferFile{Directory=true,Completed=true,Path="wrapper"}]};
  Require(!sparse.HasReceivedContent,"Summary invented a received result from an empty preview wrapper");
  sparse.HasContent=true;Require(sparse.HasReceivedContent,"Summary hid an authoritative result");
  var now=DateTimeOffset.UtcNow;
  Require(!UpdateCheckSchedule.IsDue(false,null,now) && UpdateCheckSchedule.IsDue(true,null,now) && !UpdateCheckSchedule.IsDue(true,now.AddHours(-23),now) && UpdateCheckSchedule.IsDue(true,now.AddHours(-24),now),"Optional daily check schedule is incorrect");
  return Task.CompletedTask;
 }
}
