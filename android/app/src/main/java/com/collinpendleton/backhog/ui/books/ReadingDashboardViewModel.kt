package com.collinpendleton.backhog.ui.books

import androidx.lifecycle.ViewModel
import androidx.lifecycle.ViewModelProvider
import androidx.lifecycle.viewModelScope
import com.collinpendleton.backhog.AppContainer
import com.collinpendleton.backhog.api.ReadingDebt
import com.collinpendleton.backhog.api.ReadingInsights
import com.collinpendleton.backhog.api.ReadingNow
import com.collinpendleton.backhog.api.ReadingSeason
import com.collinpendleton.backhog.api.apiCall
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch

data class ReadingDashboardState(
    val loading: Boolean = true,
    val error: String? = null,
    val insights: ReadingInsights? = null,
    val debt: ReadingDebt? = null,
    val now: ReadingNow? = null,
    val season: ReadingSeason? = null,
)

/** "Your Reading Problem": the books arena's dashboard, in four reads. */
class ReadingDashboardViewModel(
    private val container: AppContainer,
    private val baseUrl: String,
) : ViewModel() {

    private val _state = MutableStateFlow(ReadingDashboardState())
    val state: StateFlow<ReadingDashboardState> = _state.asStateFlow()

    init {
        reload()
    }

    fun reload() {
        viewModelScope.launch {
            _state.update { it.copy(loading = true, error = null) }
            apiCall { api().readingInsights() }.onSuccess { insights ->
                _state.update { it.copy(insights = insights) }
            }
            apiCall { api().readingDebt() }.onSuccess { debt ->
                _state.update { it.copy(debt = debt) }
            }
            apiCall { api().readingNow() }.onSuccess { now ->
                _state.update { it.copy(now = now) }
            }
            apiCall { api().readingSeason() }.onSuccess { season ->
                _state.update { it.copy(season = season) }
            }
            _state.update {
                it.copy(
                    loading = false,
                    error = if (it.insights == null && it.debt == null) "The dashboard could not be read." else null,
                )
            }
        }
    }

    private fun api() = container.session.api(baseUrl)

    class Factory(private val container: AppContainer, private val baseUrl: String) : ViewModelProvider.Factory {
        @Suppress("UNCHECKED_CAST")
        override fun <T : ViewModel> create(modelClass: Class<T>): T =
            ReadingDashboardViewModel(container, baseUrl) as T
    }
}
