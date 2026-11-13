use framework "Foundation"
use framework "ApplicationServices"
use scripting additions

on run
 set requestData to current application's NSFileHandle's fileHandleWithStandardInput()'s readDataToEndOfFile()
 set request to current application's NSJSONSerialization's JSONObjectWithData:requestData options:0 |error|:(missing value)
 if request is missing value then error "INVALID_SEND_REQUEST"
 set targetName to (request's objectForKey:"searchName") as text
 set messageText to (request's objectForKey:"text") as text
 if targetName is "" or messageText is "" then error "INVALID_SEND_REQUEST"
 if not (current application's AXIsProcessTrusted() as boolean) then error "ACCESSIBILITY_PERMISSION_REQUIRED"
 tell application id "com.tencent.WeWorkMac" to activate
 delay 0.5
 tell application "System Events"
  tell process "企业微信"
   set frontmost to true
   keystroke "1" using command down
   keystroke "f" using command down
   delay 0.3
   keystroke "a" using command down
   key code 51
   set the clipboard to targetName
   keystroke "v" using command down
   delay 1.5
   key code 36
   delay 1
  end tell
 end tell
 set the clipboard to messageText
 tell application "System Events"
  tell process "企业微信"
   keystroke "v" using command down
   delay 0.4
   key code 36
  end tell
 end tell
 return "submitted"
end run
