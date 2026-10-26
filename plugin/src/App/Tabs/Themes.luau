--!strict
local UiLibrary = require("../../UiLibrary")

local StudioService = game:GetService("StudioService")
local HttpService = game:GetService("HttpService")

local PREMADE_THEMES = {
	{
		Name = "Midnight",
		BackgroundColor = { 16, 18, 27 },
		ForegroundColor = { 27, 30, 43 },
		BorderColor = { 54, 61, 84 },
		UnselectedColor = { 35, 39, 56 },
		SelectedColor = { 67, 120, 255 },
		TextColor = { 230, 234, 245 },
		DimmedTextColor = { 147, 156, 183 },
		InputColor = { 230, 234, 245 },
		TipColor = { 200, 210, 235 },
		ToggleBackgroundColor = { 67, 120, 255 },
	},
	{
		Name = "Emerald",
		BackgroundColor = { 14, 24, 22 },
		ForegroundColor = { 22, 36, 33 },
		BorderColor = { 56, 92, 84 },
		UnselectedColor = { 34, 56, 51 },
		SelectedColor = { 56, 190, 145 },
		TextColor = { 229, 248, 243 },
		DimmedTextColor = { 146, 185, 173 },
		InputColor = { 220, 240, 233 },
		TipColor = { 201, 230, 220 },
		ToggleBackgroundColor = { 56, 190, 145 },
	},
	{
		Name = "Sunset",
		BackgroundColor = { 36, 25, 24 },
		ForegroundColor = { 53, 36, 35 },
		BorderColor = { 98, 74, 66 },
		UnselectedColor = { 71, 50, 46 },
		SelectedColor = { 241, 132, 82 },
		TextColor = { 253, 237, 230 },
		DimmedTextColor = { 207, 173, 159 },
		InputColor = { 250, 225, 212 },
		TipColor = { 236, 205, 192 },
		ToggleBackgroundColor = { 241, 132, 82 },
	},
	{
		Name = "Ice",
		BackgroundColor = { 222, 236, 246 },
		ForegroundColor = { 238, 246, 252 },
		BorderColor = { 170, 198, 222 },
		UnselectedColor = { 203, 223, 240 },
		SelectedColor = { 73, 142, 214 },
		TextColor = { 33, 61, 90 },
		DimmedTextColor = { 97, 128, 159 },
		InputColor = { 33, 61, 90 },
		TipColor = { 73, 104, 137 },
		ToggleBackgroundColor = { 73, 142, 214 },
		StrokeColor = { 255, 255, 255 },
		StrokeTransparency = 1,
	},
}

return function(ui: UiLibrary.Ui, _plugin: Plugin)
	local tab = ui:CreateTab("Themes")

	local function getThemeIdByName(themeName: string): string?
		for id, theme in ui.theme.getThemes() do
			if (theme :: any).Name == themeName then
				return id
			end
		end
		return nil
	end

	local function applyThemeById(themeId: string)
		local currentTheme = ui.theme.get()
		if currentTheme.Id == themeId then
			return
		end
		ui.theme.set(themeId)
		ui:UpdateTheme()
	end

	tab:CreateLabel("Pre-made themes")
	tab:CreateButton("Apply Dark Purple", function() applyThemeById("DarkPurple") end)
	tab:CreateButton("Apply Studio", function() applyThemeById("Studio") end)
	for _, rawTheme in PREMADE_THEMES do
		local themeName = rawTheme.Name
		tab:CreateButton(`Apply {themeName}`, function()
			local existingThemeId = getThemeIdByName(themeName)
			if existingThemeId then
				applyThemeById(existingThemeId)
				return
			end

			local success, newThemeId = pcall(ui.theme.add, rawTheme)
			if not success then
				local splitMessage = string.split(newThemeId :: string, ": ")
				ui:Notify("Notification", `Failed to apply {themeName}: {splitMessage[#splitMessage]}`)
				return
			end

			local themeId = newThemeId :: string
			applyThemeById(themeId)
		end)
	end

	tab:CreateButton("Import theme", function()
		local file: File = StudioService:PromptImportFile({ "json" } :: any) :: File
		if not file then
			return
		end

		local success, rawTheme = pcall(HttpService.JSONDecode, HttpService, file:GetBinaryContents())
		if not success then
			ui:Notify("Notification", "Error parsing theme. Is it a valid JSON?")
			return
		end

		local result
		success, result = pcall(ui.theme.add, rawTheme)
		if not success then
			local splitMessage = string.split(result :: string, ": ")
			ui:Notify("Notification", `Failed to import theme: {splitMessage[#splitMessage]}`)
			return
		end

		applyThemeById(result :: string)
	end)
end
