$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$OutputEncoding = [System.Text.Encoding]::UTF8

function Get-PaperName([object]$size) {
    $name = [string]$size
    switch -Regex ($name) {
        '^ISOA4' { return 'A4' }
        '^ISOA3' { return 'A3' }
        '^NorthAmericaLetter' { return 'Letter' }
        '^NorthAmericaLegal' { return 'Legal' }
    }
    return ''
}

function Get-PrinterCapabilities([System.Printing.PrintQueue]$queue) {
    $papers = New-Object System.Collections.Generic.List[string]
    $colors = New-Object System.Collections.Generic.List[string]
    $duplex = New-Object System.Collections.Generic.List[string]
    try {
        $capabilities = $queue.GetPrintCapabilities($queue.DefaultPrintTicket)
        foreach ($size in @($capabilities.PageMediaSizeCapability)) {
            $paper = Get-PaperName $size.PageMediaSizeName
            if ($paper -and -not $papers.Contains($paper)) {
                [void]$papers.Add($paper)
            }
        }
        foreach ($mode in @($capabilities.OutputColorCapability)) {
            switch ([string]$mode) {
                'Color' {
                    if (-not $colors.Contains('color')) { [void]$colors.Add('color') }
                }
                'Grayscale' {
                    if (-not $colors.Contains('monochrome')) { [void]$colors.Add('monochrome') }
                }
                'Monochrome' {
                    if (-not $colors.Contains('monochrome')) { [void]$colors.Add('monochrome') }
                }
            }
        }
        foreach ($mode in @($capabilities.DuplexingCapability)) {
            switch ([string]$mode) {
                'OneSided' {
                    if (-not $duplex.Contains('simplex')) { [void]$duplex.Add('simplex') }
                }
                'TwoSidedLongEdge' {
                    if (-not $duplex.Contains('long-edge')) { [void]$duplex.Add('long-edge') }
                }
                'TwoSidedShortEdge' {
                    if (-not $duplex.Contains('short-edge')) { [void]$duplex.Add('short-edge') }
                }
            }
        }
    } catch {
        # Some v4 and virtual drivers do not expose PrintCapabilities. The
        # conservative defaults remain usable and are validated again when a
        # job is submitted.
    }
    if ($papers.Count -eq 0) {
        @('A4', 'A3', 'Letter') | ForEach-Object { [void]$papers.Add($_) }
    }
    if ($colors.Count -eq 0) {
        @('color', 'monochrome') | ForEach-Object { [void]$colors.Add($_) }
    }
    if ($duplex.Count -eq 0) {
        @('simplex', 'long-edge', 'short-edge') | ForEach-Object { [void]$duplex.Add($_) }
    }
    return [pscustomobject]@{
        papers = @($papers)
        colors = @($colors)
        duplex = @($duplex)
        orientations = @('portrait', 'landscape')
    }
}

function Write-PrinterCatalog {
    Add-Type -AssemblyName System.Printing
    $server = New-Object System.Printing.LocalPrintServer
    try {
        $default = [string](Get-CimInstance Win32_Printer -ErrorAction SilentlyContinue |
            Where-Object { $_.Default } | Select-Object -First 1 -ExpandProperty Name)
        $rows = New-Object System.Collections.ArrayList
        foreach ($printer in @(Get-Printer)) {
            $queue = $null
            try {
                $queue = $server.GetPrintQueue([string]$printer.Name)
            } catch {
            }
            if ($null -eq $queue) {
                continue
            }
            $capabilities = Get-PrinterCapabilities $queue
            $status = [string]$printer.PrinterStatus
            [void]$rows.Add([pscustomobject]@{
                id = [string]$printer.Name
                name = [string]$printer.Name
                available = (-not [bool]$printer.WorkOffline) -and
                    $status -notin @('Error', 'NotAvailable', 'Offline')
                default = ([string]$printer.Name -eq $default)
                status = $status
                papers = @($capabilities.papers)
                colors = @($capabilities.colors)
                duplex = @($capabilities.duplex)
                orientations = @($capabilities.orientations)
            })
            $queue.Dispose()
        }
        ConvertTo-Json -InputObject @($rows) -Depth 6 -Compress
    } finally {
        $server.Dispose()
    }
}

function Initialize-PdfTypes {
    Add-Type -AssemblyName System.Runtime.WindowsRuntime
    $null = [Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime]
    $null = [Windows.Data.Pdf.PdfDocument, Windows.Data.Pdf, ContentType = WindowsRuntime]
    $null = [Windows.Data.Pdf.PdfPageRenderOptions, Windows.Data.Pdf, ContentType = WindowsRuntime]
    $null = [Windows.Storage.Streams.InMemoryRandomAccessStream, Windows.Storage.Streams, ContentType = WindowsRuntime]
    $null = [Windows.Storage.Streams.DataReader, Windows.Storage.Streams, ContentType = WindowsRuntime]
    $script:AsTaskGeneric = ([System.WindowsRuntimeSystemExtensions].GetMethods() |
        Where-Object {
            $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
            $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation`1'
        })[0]
    $script:AsTaskAction = ([System.WindowsRuntimeSystemExtensions].GetMethods() |
        Where-Object {
            $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and
            $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncAction'
        })[0]
    if ($null -eq $script:AsTaskGeneric -or $null -eq $script:AsTaskAction) {
        throw 'Windows PDF runtime is unavailable.'
    }
}

function Wait-WinRt($operation, [Type]$resultType) {
    $method = $script:AsTaskGeneric.MakeGenericMethod($resultType)
    $task = $method.Invoke($null, @($operation))
    try {
        $task.Wait(-1) | Out-Null
    } catch {
        if ($null -ne $_.Exception.InnerException) {
            throw $_.Exception.InnerException
        }
        throw
    }
    return $task.Result
}

function Wait-WinRtAction($operation) {
    $task = $script:AsTaskAction.Invoke($null, @($operation))
    try {
        $task.Wait(-1) | Out-Null
    } catch {
        if ($null -ne $_.Exception.InnerException) {
            throw $_.Exception.InnerException
        }
        throw
    }
}

function Open-Pdf([string]$path) {
    $file = Wait-WinRt ([Windows.Storage.StorageFile]::GetFileFromPathAsync($path)) ([Windows.Storage.StorageFile])
    return Wait-WinRt ([Windows.Data.Pdf.PdfDocument]::LoadFromFileAsync($file)) ([Windows.Data.Pdf.PdfDocument])
}

function Get-SelectedPages([string]$value, [int]$count) {
    $value = ($value -replace '\s', '')
    if ([string]::IsNullOrEmpty($value)) {
        return @(1..$count)
    }
    $selected = New-Object System.Collections.Generic.List[int]
    foreach ($part in $value.Split(',')) {
        $limits = $part.Split('-')
        $first = 0
        $last = 0
        if (-not [int]::TryParse($limits[0], [ref]$first)) {
            throw '页码范围格式无效。'
        }
        $last = $first
        if ($limits.Count -eq 2 -and -not [int]::TryParse($limits[1], [ref]$last)) {
            throw '页码范围格式无效。'
        }
        if ($first -lt 1 -or $last -lt $first -or $last -gt $count) {
            throw "页码范围超出文档页数（共 $count 页）。"
        }
        for ($page = $first; $page -le $last; $page++) {
            if (-not $selected.Contains($page)) {
                [void]$selected.Add($page)
            }
        }
    }
    if ($selected.Count -eq 0) {
        throw '没有可打印的页面。'
    }
    return @($selected)
}

function Get-EdgePath {
    $candidates = @(
        (Join-Path ${env:ProgramFiles(x86)} 'Microsoft\Edge\Application\msedge.exe'),
        (Join-Path $env:ProgramFiles 'Microsoft\Edge\Application\msedge.exe'),
        (Join-Path $env:LOCALAPPDATA 'Microsoft\Edge\Application\msedge.exe')
    )
    foreach ($candidate in $candidates) {
        if ($candidate -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return $candidate
        }
    }
    return $null
}

function Convert-DocumentToPdf([string]$source, [string]$destination, [string]$profile) {
    $edge = Get-EdgePath
    if ($null -eq $edge) {
        throw '此文件需要 Microsoft Edge 转换为可打印页面，但系统未找到 Edge。'
    }
    $url = ([System.Uri]::new($source)).AbsoluteUri
    $arguments = @(
        '--headless',
        '--disable-gpu',
        '--no-pdf-header-footer',
        "--user-data-dir=$profile",
        "--print-to-pdf=$destination",
        $url
    )
    & $edge @arguments 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $destination -PathType Leaf)) {
        throw 'Microsoft Edge 无法将此文件转换为可打印页面。'
    }
}

function Save-PdfPage([object]$document, [uint32]$index, [string]$destination) {
    $page = $document.GetPage($index)
    $stream = $null
    $reader = $null
    try {
        $options = [Windows.Data.Pdf.PdfPageRenderOptions]::new()
        $size = $page.Size
        $options.DestinationWidth = [uint32][Math]::Max(600, [Math]::Min(10000, $size.Width * 200 / 72))
        $options.DestinationHeight = [uint32][Math]::Max(600, [Math]::Min(10000, $size.Height * 200 / 72))
        $stream = [Windows.Storage.Streams.InMemoryRandomAccessStream]::new()
        Wait-WinRtAction ($page.RenderToStreamAsync($stream, $options))
        $length = [uint32]$stream.Size
        $reader = [Windows.Storage.Streams.DataReader]::new($stream.GetInputStreamAt(0))
        Wait-WinRt ($reader.LoadAsync($length)) ([uint32]) | Out-Null
        $bytes = New-Object byte[] $length
        $reader.ReadBytes($bytes)
        [System.IO.File]::WriteAllBytes($destination, $bytes)
    } finally {
        if ($null -ne $reader) { $reader.Dispose() }
        if ($null -ne $stream) { $stream.Dispose() }
        $page.Dispose()
    }
}

function Save-RasterPage([string]$source, [string]$destination, [bool]$landscape) {
    Add-Type -AssemblyName System.Drawing
    $image = [System.Drawing.Image]::FromFile($source)
    try {
        $sourceLandscape = $image.Width -gt $image.Height
        $width = $image.Width
        $height = $image.Height
        $bitmap = $null
        $graphics = $null
        try {
            if ($sourceLandscape -ne $landscape) {
                $bitmap = New-Object System.Drawing.Bitmap $height, $width,
                    ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
                $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
                $graphics.Clear([System.Drawing.Color]::White)
                $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $graphics.TranslateTransform($height, 0)
                $graphics.RotateTransform(90)
                $graphics.DrawImage($image, 0, 0, $width, $height)
            } else {
                $bitmap = New-Object System.Drawing.Bitmap $width, $height,
                    ([System.Drawing.Imaging.PixelFormat]::Format32bppArgb)
                $graphics = [System.Drawing.Graphics]::FromImage($bitmap)
                $graphics.Clear([System.Drawing.Color]::White)
                $graphics.InterpolationMode = [System.Drawing.Drawing2D.InterpolationMode]::HighQualityBicubic
                $graphics.DrawImage($image, 0, 0, $width, $height)
            }
            $bitmap.Save($destination, [System.Drawing.Imaging.ImageFormat]::Png)
        } finally {
            if ($null -ne $graphics) { $graphics.Dispose() }
            if ($null -ne $bitmap) { $bitmap.Dispose() }
        }
    } finally {
        $image.Dispose()
    }
}

function Get-PaperDimensions([string]$paper) {
    switch ($paper) {
        'A3' { return @(1122.52, 1587.40) }
        'Letter' { return @(816.0, 1056.0) }
        'Legal' { return @(816.0, 1344.0) }
        default { return @(793.70, 1122.52) }
    }
}

function Get-MediaSize([string]$paper) {
    switch ($paper) {
        'A3' { return 'ISOA3' }
        'Letter' { return 'NorthAmericaLetter' }
        'Legal' { return 'NorthAmericaLegal' }
        default { return 'ISOA4' }
    }
}

function New-PrintTicket(
    [System.Printing.PrintQueue]$queue,
    [string]$paper,
    [string]$orientation,
    [string]$color,
    [string]$duplex
) {
    $ticket = $queue.DefaultPrintTicket.Clone()
    $ticket.CopyCount = $copies
    $ticket.PageMediaSize = New-Object System.Printing.PageMediaSize(
        ([System.Printing.PageMediaSizeName](Get-MediaSize $paper)))
    if ($orientation -eq 'landscape') {
        $ticket.PageOrientation = [System.Printing.PageOrientation]::Landscape
    } else {
        $ticket.PageOrientation = [System.Printing.PageOrientation]::Portrait
    }
    if ($color -eq 'color') {
        $ticket.OutputColor = [System.Printing.OutputColor]::Color
    } elseif ($color -eq 'monochrome') {
        $ticket.OutputColor = [System.Printing.OutputColor]::Monochrome
    }
    switch ($duplex) {
        'long-edge' { $ticket.Duplexing = [System.Printing.Duplexing]::TwoSidedLongEdge }
        'short-edge' { $ticket.Duplexing = [System.Printing.Duplexing]::TwoSidedShortEdge }
        default { $ticket.Duplexing = [System.Printing.Duplexing]::OneSided }
    }
    $validation = $queue.MergeAndValidatePrintTicket($queue.DefaultPrintTicket, $ticket)
    if ($null -ne $validation.ValidatedPrintTicket) {
        return $validation.ValidatedPrintTicket
    }
    return $ticket
}

function New-PrintXps([string[]]$images, [string]$destination, [string]$paper, [string]$orientation, [int]$copies) {
    Add-Type -AssemblyName ReachFramework
    Add-Type -AssemblyName PresentationCore
    Add-Type -AssemblyName PresentationFramework
    Add-Type -AssemblyName WindowsBase
    $dimensions = Get-PaperDimensions $paper
    $width = [double]$dimensions[0]
    $height = [double]$dimensions[1]
    if ($orientation -eq 'landscape') {
        $swap = $width
        $width = $height
        $height = $swap
    }
    $document = New-Object System.Windows.Xps.Packaging.XpsDocument(
        $destination, [System.IO.FileAccess]::ReadWrite)
    try {
        $writer = [System.Windows.Xps.Packaging.XpsDocument]::CreateXpsDocumentWriter($document)
        $fixedDocument = New-Object System.Windows.Documents.FixedDocument
        $sources = New-Object System.Collections.Generic.List[object]
        foreach ($path in $images) {
            $bitmap = New-Object System.Windows.Media.Imaging.BitmapImage
            $bitmap.BeginInit()
            $bitmap.CacheOption = [System.Windows.Media.Imaging.BitmapCacheOption]::OnLoad
            $bitmap.UriSource = [System.Uri]::new($path)
            $bitmap.EndInit()
            $bitmap.Freeze()
            [void]$sources.Add($bitmap)
        }
        foreach ($source in $sources) {
            $page = New-Object System.Windows.Documents.FixedPage
            $page.Width = $width
            $page.Height = $height
            $background = New-Object System.Windows.Shapes.Rectangle
            $background.Width = $width
            $background.Height = $height
            $background.Fill = [System.Windows.Media.Brushes]::White
            $page.Children.Add($background) | Out-Null
            $image = New-Object System.Windows.Controls.Image
            $image.Source = $source
            $image.Width = $width
            $image.Height = $height
            $image.Stretch = [System.Windows.Media.Stretch]::Uniform
            [System.Windows.Media.RenderOptions]::SetBitmapScalingMode(
                $image, [System.Windows.Media.BitmapScalingMode]::HighQuality)
            $page.Children.Add($image) | Out-Null
            $content = New-Object System.Windows.Documents.PageContent
            $content.Child = $page
            $fixedDocument.Pages.Add($content) | Out-Null
        }
        $writer.Write($fixedDocument)
    } finally {
        $document.Close()
    }
}

function Submit-PrintJob {
    $printer = [string]$env:GOIM_PRINT_PRINTER
    $source = [string]$env:GOIM_PRINT_FILE
    $paper = [string]$env:GOIM_PRINT_PAPER
    $orientation = [string]$env:GOIM_PRINT_ORIENTATION
    $duplex = [string]$env:GOIM_PRINT_DUPLEX
    $color = [string]$env:GOIM_PRINT_COLOR
    $range = [string]$env:GOIM_PRINT_RANGE
    $copies = [int]$env:GOIM_PRINT_COPIES
    if ([string]::IsNullOrWhiteSpace($printer)) {
        throw 'No printer selected.'
    }
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw 'The selected file is no longer available.'
    }
    if ($copies -lt 1 -or $copies -gt 999) {
        throw '打印份数需在 1 到 999 之间。'
    }
    $source = (Resolve-Path -LiteralPath $source).Path
    $extension = [System.IO.Path]::GetExtension($source).ToLowerInvariant()
    $landscape = $orientation -eq 'landscape'
    $working = Join-Path ([System.IO.Path]::GetTempPath()) (
        'GoIM-Print-' + [Guid]::NewGuid().ToString('N'))
    [System.IO.Directory]::CreateDirectory($working) | Out-Null
    $jobID = $null
    try {
        $pages = New-Object System.Collections.Generic.List[string]
        switch ($extension) {
            '.pdf' {
                Initialize-PdfTypes
                $document = Open-Pdf $source
                $selected = @(Get-SelectedPages $range ([int]$document.PageCount))
                $number = 1
                foreach ($pageNumber in $selected) {
                    $output = Join-Path $working ('page-{0:D4}.png' -f $number)
                    Save-PdfPage $document ([uint32]($pageNumber - 1)) $output
                    [void]$pages.Add($output)
                    $number++
                }
            }
            '.txt' {
                Initialize-PdfTypes
                $pdf = Join-Path $working 'document.pdf'
                Convert-DocumentToPdf $source $pdf (Join-Path $working 'edge-profile')
                $document = Open-Pdf $pdf
                $selected = @(Get-SelectedPages $range ([int]$document.PageCount))
                $number = 1
                foreach ($pageNumber in $selected) {
                    $output = Join-Path $working ('page-{0:D4}.png' -f $number)
                    Save-PdfPage $document ([uint32]($pageNumber - 1)) $output
                    [void]$pages.Add($output)
                    $number++
                }
            }
            { $_ -in @('.png', '.jpg', '.jpeg', '.bmp', '.gif', '.tif', '.tiff') } {
                $selected = @(Get-SelectedPages $range 1)
                if ($selected.Count -ne 1 -or $selected[0] -ne 1) {
                    throw '此图片只有 1 页，页码范围只能为 1。'
                }
                $output = Join-Path $working 'page-0001.png'
                Save-RasterPage $source $output $landscape
                [void]$pages.Add($output)
            }
            default {
                throw 'Windows 打印暂支持 PDF、PNG、JPEG、BMP、GIF、TIFF 和 TXT 文件。'
            }
        }
        if ($pages.Count -eq 0) {
            throw '没有可打印的页面。'
        }
        $xps = Join-Path $working 'document.xps'
        New-PrintXps @($pages) $xps $paper $orientation 1

        Add-Type -AssemblyName System.Printing
        $server = New-Object System.Printing.LocalPrintServer
        $queue = $null
        try {
            $queue = $server.GetPrintQueue($printer)
            $ticket = New-PrintTicket $queue $paper $orientation $color $duplex
            $documentName = 'GoIM ' + [Guid]::NewGuid().ToString('N')
            $job = $queue.AddJob($documentName, $xps, $false, $ticket)
            if ($null -eq $job) {
                throw 'The Windows spooler did not return a print job.'
            }
            $jobID = [string]$job.JobIdentifier
        } finally {
            if ($null -ne $queue) { $queue.Dispose() }
            $server.Dispose()
        }
    } finally {
        Remove-Item -LiteralPath $working -Recurse -Force -ErrorAction SilentlyContinue
    }
    if ([string]::IsNullOrWhiteSpace($jobID)) {
        throw 'The Windows spooler did not return a print job identifier.'
    }
    [Console]::Out.Write($jobID)
}

function Write-PrintJobStatus {
    Add-Type -AssemblyName System.Printing
    $printer = [string]$env:GOIM_PRINT_PRINTER
    $jobID = 0
    if (-not [int]::TryParse([string]$env:GOIM_PRINT_JOB, [ref]$jobID)) {
        Write-Output 'unknown'
        return
    }
    $server = New-Object System.Printing.LocalPrintServer
    $queue = $null
    try {
        $queue = $server.GetPrintQueue($printer)
        if ($env:GOIM_PRINT_MODE -eq 'cancel') {
            $job = $queue.GetJob($jobID)
            $job.Refresh()
            if ($job.IsDeleted -or $job.IsCompleted -or $job.IsPrinted) { throw 'The job has already ended.' }
            $job.Cancel()
            return
        }
        $job = $null
        try {
            $job = $queue.GetJob($jobID)
        } catch [System.Printing.PrintQueueException] {
            Write-Output 'unknown'
            return
        }
        if ($job.IsInError -or $job.IsDeleted) {
            Write-Output 'failed'
        } elseif ($job.IsPrinted -or $job.IsCompleted) {
            Write-Output 'completed'
        } else {
            Write-Output 'queued'
        }
    } finally {
        if ($null -ne $queue) { $queue.Dispose() }
        $server.Dispose()
    }
}

switch ([string]$env:GOIM_PRINT_MODE) {
    'list' { Write-PrinterCatalog }
    'submit' { Submit-PrintJob }
    'status' { Write-PrintJobStatus }
    'cancel' { Write-PrintJobStatus }
    default { throw 'Unknown print helper mode.' }
}
