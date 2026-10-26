--!strict
local UiLibrary = require("./Plugin/UiLibrary")
local App = require("./Plugin/App")

local RunService = game:GetService("RunService")

if not RunService:IsEdit() then
	return
end

local currentVersion = script.Parent.Version.Value
local topbarTitle = "Uploaderv3Newest"

local toolbar = plugin:CreateToolbar("Uploaderv3Newest")
local widget = plugin:CreateDockWidgetPluginGui(
	"Uploaderv3Newest",
	DockWidgetPluginGuiInfo.new(Enum.InitialDockState.Float, false, true, 500, 250, 500, 250)
)

local toggleButton = toolbar:CreateButton("Toggle Gui", "Toggle gui visibility", "rbxassetid://83178708216253")
toggleButton.Click:Connect(function() widget.Enabled = not widget.Enabled end)

widget:GetPropertyChangedSignal("Enabled"):Connect(function() toggleButton:SetActive(widget.Enabled) end)
widget.Title = topbarTitle

if plugin:GetSetting("Port") == nil then
	plugin:SetSetting("Port", 38073)
end

local ui = UiLibrary.new(widget, plugin)
App.init(ui, plugin)

plugin.Unloading:Once(function()
	ui:Destroy()
end)
