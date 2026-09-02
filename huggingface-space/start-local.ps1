$ErrorActionPreference = 'Stop'
$workspacePath = Split-Path (Split-Path $PSScriptRoot -Parent) -Parent
$pythonPath = Join-Path $workspacePath '.artqr-venv/Scripts/python.exe'
if (-not (Test-Path -LiteralPath $pythonPath)) {
    throw 'Install the .artqr-venv environment first.'
}
$env:ART_QR_LOCAL = '1'
$env:HF_HOME = Join-Path $workspacePath '.artqr-models'
$env:GRADIO_ANALYTICS_ENABLED = 'False'
& $pythonPath (Join-Path $PSScriptRoot 'app.py')
